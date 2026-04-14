// chain.go implements an Ed25519-signed hash chain for the token ledger.
//
// Zero-trust design: every token event is chained (SHA-256 of previous event),
// signed by HQ's Ed25519 key, and optionally counter-signed by the factory node.
// Either party can independently verify the entire chain. Tampering with any
// single row breaks the chain from that point forward.
//
// 零信任令牌账本 — 每笔交易由总部签名，工厂节点可以反签，任何篡改都会断链。
package persist

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// SignedEvent is a token event with hash chain and cryptographic signatures.
type SignedEvent struct {
	Seq        int64  `json:"seq"`         // monotonic sequence number
	PublicKey  string `json:"public_key"`  // factory Ed25519 pubkey (hex)
	Amount     int64  `json:"amount"`      // positive = earn/grant, negative = spend
	Balance    int64  `json:"balance"`     // balance AFTER this event
	EventType  string `json:"event_type"`  // earn, spend, grant, pair
	Reason     string `json:"reason"`
	Timestamp  string `json:"timestamp"`   // RFC3339 UTC
	PrevHash   string `json:"prev_hash"`   // SHA-256 of previous event's canonical form
	EventHash  string `json:"event_hash"`  // SHA-256 of this event's canonical form (without signatures)
	HQSig      string `json:"hq_sig"`      // HQ Ed25519 signature over event_hash
	CounterSig string `json:"counter_sig"` // Factory's Ed25519 signature over event_hash (optional)
}

// Receipt is the portable proof a factory node keeps locally.
// It contains everything needed to independently verify one transaction.
type Receipt struct {
	Seq       int64  `json:"seq"`
	PublicKey string `json:"public_key"`
	Amount    int64  `json:"amount"`
	Balance   int64  `json:"balance"`
	EventType string `json:"event_type"`
	Reason    string `json:"reason"`
	Timestamp string `json:"timestamp"`
	PrevHash  string `json:"prev_hash"`
	EventHash string `json:"event_hash"`
	HQSig     string `json:"hq_sig"`
	HQPubKey  string `json:"hq_pub_key"` // so factory can verify without trusting HQ endpoint
}

// canonicalForm produces the deterministic byte string that gets hashed.
// This is the "contract" — both HQ and factory must agree on this format.
// No signatures included (they sign the hash of this).
func canonicalForm(seq int64, pubKey string, amount, balance int64, eventType, reason, timestamp, prevHash string) []byte {
	return []byte(fmt.Sprintf("%d|%s|%d|%d|%s|%s|%s|%s",
		seq, pubKey, amount, balance, eventType, reason, timestamp, prevHash))
}

// hashEvent computes SHA-256 of the canonical form.
func hashEvent(seq int64, pubKey string, amount, balance int64, eventType, reason, timestamp, prevHash string) string {
	data := canonicalForm(seq, pubKey, amount, balance, eventType, reason, timestamp, prevHash)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// signHash signs a hex-encoded hash with an Ed25519 private key.
func signHash(hash string, priv ed25519.PrivateKey) string {
	hashBytes, err := hex.DecodeString(hash)
	if err != nil {
		return ""
	}
	sig := ed25519.Sign(priv, hashBytes)
	return hex.EncodeToString(sig)
}

// VerifyEventSig verifies an Ed25519 signature on an event hash.
func VerifyEventSig(eventHash, signature string, pub ed25519.PublicKey) bool {
	hashBytes, err := hex.DecodeString(eventHash)
	if err != nil {
		return false
	}
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, hashBytes, sigBytes)
}

// ── Schema migration for signed chain ────────────────────────────────────────

func (s *Store) migrateChain() error {
	schema := `
	CREATE TABLE IF NOT EXISTS signed_events (
		seq         INTEGER PRIMARY KEY AUTOINCREMENT,
		public_key  TEXT NOT NULL,
		amount      INTEGER NOT NULL,
		balance     INTEGER NOT NULL,
		event_type  TEXT NOT NULL,
		reason      TEXT NOT NULL DEFAULT '',
		timestamp   TEXT NOT NULL,
		prev_hash   TEXT NOT NULL,
		event_hash  TEXT NOT NULL,
		hq_sig      TEXT NOT NULL,
		counter_sig TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_signed_events_pubkey ON signed_events(public_key);
	CREATE INDEX IF NOT EXISTS idx_signed_events_hash ON signed_events(event_hash);
	`
	_, err := s.db.Exec(schema)
	return err
}

// ── Chain Operations ─────────────────────────────────────────────────────────

// lastChainHash returns the event_hash of the most recent signed event,
// or the genesis hash "0" if the chain is empty.
func (s *Store) lastChainHash(tx *sql.Tx) (string, error) {
	var hash string
	err := tx.QueryRow(`SELECT event_hash FROM signed_events ORDER BY seq DESC LIMIT 1`).Scan(&hash)
	if err == sql.ErrNoRows {
		return "0", nil
	}
	return hash, err
}

// AppendSignedEvent atomically adjusts balance and appends a signed chain event.
// Returns the signed event (which doubles as a receipt for the factory).
func (s *Store) AppendSignedEvent(pubKey string, delta int64, eventType, reason string, hqPriv ed25519.PrivateKey) (*SignedEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Get current balance
	var bal int64
	err = tx.QueryRow(`SELECT balance FROM balances WHERE public_key = ?`, pubKey).Scan(&bal)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("read balance: %w", err)
	}

	newBal := bal + delta
	if newBal < 0 {
		return nil, fmt.Errorf("insufficient tokens: have %d, need %d", bal, -delta)
	}

	// Update balance
	_, err = tx.Exec(`
		INSERT INTO balances (public_key, balance) VALUES (?, ?)
		ON CONFLICT(public_key) DO UPDATE SET balance=excluded.balance`,
		pubKey, newBal)
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}

	// Get previous chain hash
	prevHash, err := s.lastChainHash(tx)
	if err != nil {
		return nil, fmt.Errorf("last chain hash: %w", err)
	}

	// Build the event
	now := time.Now().UTC().Format(time.RFC3339)

	// We need the seq number — use a temporary insert to get it
	result, err := tx.Exec(`
		INSERT INTO signed_events (public_key, amount, balance, event_type, reason, timestamp, prev_hash, event_hash, hq_sig)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', '')`,
		pubKey, delta, newBal, eventType, reason, now, prevHash)
	if err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}
	seq, _ := result.LastInsertId()

	// Compute hash and signature
	eventHash := hashEvent(seq, pubKey, delta, newBal, eventType, reason, now, prevHash)
	hqSig := signHash(eventHash, hqPriv)

	// Update with computed hash and signature
	_, err = tx.Exec(`UPDATE signed_events SET event_hash = ?, hq_sig = ? WHERE seq = ?`,
		eventHash, hqSig, seq)
	if err != nil {
		return nil, fmt.Errorf("update event hash: %w", err)
	}

	// Also write to the plain token_events table for backward compat
	_, err = tx.Exec(`INSERT INTO token_events (public_key, amount, event_type, reason, timestamp) VALUES (?, ?, ?, ?, ?)`,
		pubKey, delta, eventType, reason, now)
	if err != nil {
		return nil, fmt.Errorf("insert legacy event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &SignedEvent{
		Seq:       seq,
		PublicKey: pubKey,
		Amount:    delta,
		Balance:   newBal,
		EventType: eventType,
		Reason:    reason,
		Timestamp: now,
		PrevHash:  prevHash,
		EventHash: eventHash,
		HQSig:     hqSig,
	}, nil
}

// CounterSign adds a factory node's counter-signature to an event.
// The factory signs the same event_hash, proving it acknowledges the transaction.
func (s *Store) CounterSign(seq int64, counterSig string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`UPDATE signed_events SET counter_sig = ? WHERE seq = ?`, counterSig, seq)
	if err != nil {
		return fmt.Errorf("counter sign: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("event seq %d not found", seq)
	}
	return nil
}

// ── Chain Verification ───────────────────────────────────────────────────────

// ChainIntegrity walks the entire chain and verifies:
// 1. Each event_hash matches recomputed hash from canonical form
// 2. Each prev_hash matches the previous event's event_hash
// 3. Each HQ signature is valid
// 4. Running balance is consistent
//
// Returns (lastVerifiedSeq, nil) on success, or (failedSeq, error) on failure.
func (s *Store) ChainIntegrity(hqPub ed25519.PublicKey) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT seq, public_key, amount, balance, event_type, reason, timestamp, prev_hash, event_hash, hq_sig FROM signed_events ORDER BY seq ASC`)
	if err != nil {
		return 0, fmt.Errorf("query chain: %w", err)
	}
	defer rows.Close()

	expectedPrevHash := "0"
	balances := make(map[string]int64)
	var lastSeq int64

	for rows.Next() {
		var e SignedEvent
		if err := rows.Scan(&e.Seq, &e.PublicKey, &e.Amount, &e.Balance, &e.EventType, &e.Reason, &e.Timestamp, &e.PrevHash, &e.EventHash, &e.HQSig); err != nil {
			return lastSeq, fmt.Errorf("scan seq %d: %w", e.Seq, err)
		}

		// 1. Verify chain link
		if e.PrevHash != expectedPrevHash {
			return e.Seq, fmt.Errorf("seq %d: chain broken — prev_hash %s, expected %s", e.Seq, e.PrevHash, expectedPrevHash)
		}

		// 2. Verify hash matches canonical form
		recomputed := hashEvent(e.Seq, e.PublicKey, e.Amount, e.Balance, e.EventType, e.Reason, e.Timestamp, e.PrevHash)
		if recomputed != e.EventHash {
			return e.Seq, fmt.Errorf("seq %d: hash mismatch — stored %s, recomputed %s", e.Seq, e.EventHash, recomputed)
		}

		// 3. Verify HQ signature
		if !VerifyEventSig(e.EventHash, e.HQSig, hqPub) {
			return e.Seq, fmt.Errorf("seq %d: invalid HQ signature", e.Seq)
		}

		// 4. Verify balance consistency
		balances[e.PublicKey] += e.Amount
		if balances[e.PublicKey] != e.Balance {
			return e.Seq, fmt.Errorf("seq %d: balance mismatch — chain says %d, running total %d",
				e.Seq, e.Balance, balances[e.PublicKey])
		}

		expectedPrevHash = e.EventHash
		lastSeq = e.Seq
	}

	return lastSeq, nil
}

// ── Query signed events ──────────────────────────────────────────────────────

// SignedEventsFor returns all signed events for a given public key.
// These are the factory's receipts — independently verifiable.
func (s *Store) SignedEventsFor(pubKey string, limit int) ([]SignedEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT seq, public_key, amount, balance, event_type, reason, timestamp, prev_hash, event_hash, hq_sig, counter_sig FROM signed_events WHERE public_key = ? ORDER BY seq DESC LIMIT ?`, pubKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SignedEvent
	for rows.Next() {
		var e SignedEvent
		if err := rows.Scan(&e.Seq, &e.PublicKey, &e.Amount, &e.Balance, &e.EventType, &e.Reason, &e.Timestamp, &e.PrevHash, &e.EventHash, &e.HQSig, &e.CounterSig); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// FullChain returns the entire signed event chain in order.
// Anyone can download and verify independently.
func (s *Store) FullChain(limit int) ([]SignedEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT seq, public_key, amount, balance, event_type, reason, timestamp, prev_hash, event_hash, hq_sig, counter_sig FROM signed_events ORDER BY seq ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SignedEvent
	for rows.Next() {
		var e SignedEvent
		if err := rows.Scan(&e.Seq, &e.PublicKey, &e.Amount, &e.Balance, &e.EventType, &e.Reason, &e.Timestamp, &e.PrevHash, &e.EventHash, &e.HQSig, &e.CounterSig); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// ToReceipt converts a signed event into a portable receipt for the factory node.
func (e *SignedEvent) ToReceipt(hqPubKey ed25519.PublicKey) Receipt {
	return Receipt{
		Seq:       e.Seq,
		PublicKey: e.PublicKey,
		Amount:    e.Amount,
		Balance:   e.Balance,
		EventType: e.EventType,
		Reason:    e.Reason,
		Timestamp: e.Timestamp,
		PrevHash:  e.PrevHash,
		EventHash: e.EventHash,
		HQSig:     e.HQSig,
		HQPubKey:  hex.EncodeToString(hqPubKey),
	}
}

// VerifyReceipt independently verifies a receipt using only the data it contains.
// No database access needed — pure cryptographic verification.
func VerifyReceipt(r *Receipt) error {
	// Recompute hash from canonical form
	recomputed := hashEvent(r.Seq, r.PublicKey, r.Amount, r.Balance, r.EventType, r.Reason, r.Timestamp, r.PrevHash)
	if recomputed != r.EventHash {
		return fmt.Errorf("receipt hash mismatch: computed %s, got %s", recomputed, r.EventHash)
	}

	// Verify HQ signature
	pubBytes, err := hex.DecodeString(r.HQPubKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid HQ public key")
	}
	if !VerifyEventSig(r.EventHash, r.HQSig, ed25519.PublicKey(pubBytes)) {
		return fmt.Errorf("invalid HQ signature on receipt")
	}

	return nil
}
