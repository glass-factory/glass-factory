package persist

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"
)

func tempChainDB(t *testing.T) (*Store, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.migrateChain(); err != nil {
		t.Fatalf("migrateChain: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return s, pub, priv
}

var testPK = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// ── Basic chain append and verify ───────────────────────────────────────────

func TestAppendSignedEvent(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	// Seed a balance so we can also test spending
	s.SetBalance(testPK, 0, "init", "test setup")

	ev, err := s.AppendSignedEvent(testPK, 1000, "grant", "early adopter", hqPriv)
	if err != nil {
		t.Fatalf("AppendSignedEvent: %v", err)
	}

	if ev.Seq != 1 {
		t.Errorf("Seq = %d, want 1", ev.Seq)
	}
	if ev.Balance != 1000 {
		t.Errorf("Balance = %d, want 1000", ev.Balance)
	}
	if ev.PrevHash != "0" {
		t.Errorf("PrevHash = %q, want genesis '0'", ev.PrevHash)
	}
	if ev.EventHash == "" {
		t.Error("EventHash is empty")
	}
	if ev.HQSig == "" {
		t.Error("HQSig is empty")
	}

	// Verify HQ signature
	if !VerifyEventSig(ev.EventHash, ev.HQSig, hqPub) {
		t.Error("HQ signature verification failed")
	}
}

// ── Chain links correctly ───────────────────────────────────────────────────

func TestChainLinks(t *testing.T) {
	s, _, hqPriv := tempChainDB(t)

	ev1, err := s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)
	if err != nil {
		t.Fatalf("event 1: %v", err)
	}

	ev2, err := s.AppendSignedEvent(testPK, 500, "earn", "job-001", hqPriv)
	if err != nil {
		t.Fatalf("event 2: %v", err)
	}

	ev3, err := s.AppendSignedEvent(testPK, -200, "spend", "build-001", hqPriv)
	if err != nil {
		t.Fatalf("event 3: %v", err)
	}

	// Chain links
	if ev2.PrevHash != ev1.EventHash {
		t.Errorf("ev2.PrevHash = %s, want ev1.EventHash = %s", ev2.PrevHash, ev1.EventHash)
	}
	if ev3.PrevHash != ev2.EventHash {
		t.Errorf("ev3.PrevHash = %s, want ev2.EventHash = %s", ev3.PrevHash, ev2.EventHash)
	}

	// Balance progression
	if ev1.Balance != 1000 {
		t.Errorf("ev1.Balance = %d, want 1000", ev1.Balance)
	}
	if ev2.Balance != 1500 {
		t.Errorf("ev2.Balance = %d, want 1500", ev2.Balance)
	}
	if ev3.Balance != 1300 {
		t.Errorf("ev3.Balance = %d, want 1300", ev3.Balance)
	}
}

// ── Chain integrity verification ────────────────────────────────────────────

func TestChainIntegrity_Valid(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)
	s.AppendSignedEvent(testPK, 500, "earn", "second", hqPriv)
	s.AppendSignedEvent(testPK, -200, "spend", "third", hqPriv)

	lastSeq, err := s.ChainIntegrity(hqPub)
	if err != nil {
		t.Fatalf("ChainIntegrity: %v", err)
	}
	if lastSeq != 3 {
		t.Errorf("lastSeq = %d, want 3", lastSeq)
	}
}

func TestChainIntegrity_TamperedHash(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)
	s.AppendSignedEvent(testPK, 500, "earn", "second", hqPriv)

	// Tamper with event 2's hash directly in DB
	s.mu.Lock()
	s.db.Exec(`UPDATE signed_events SET event_hash = 'deadbeef' WHERE seq = 2`)
	s.mu.Unlock()

	_, err := s.ChainIntegrity(hqPub)
	if err == nil {
		t.Fatal("expected chain integrity failure after tampering hash")
	}
}

func TestChainIntegrity_TamperedAmount(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)
	s.AppendSignedEvent(testPK, 500, "earn", "second", hqPriv)

	// Tamper with event 1's amount (try to give yourself more tokens)
	s.mu.Lock()
	s.db.Exec(`UPDATE signed_events SET amount = 999999 WHERE seq = 1`)
	s.mu.Unlock()

	_, err := s.ChainIntegrity(hqPub)
	if err == nil {
		t.Fatal("expected chain integrity failure after tampering amount")
	}
}

func TestChainIntegrity_TamperedBalance(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)

	// Tamper with balance
	s.mu.Lock()
	s.db.Exec(`UPDATE signed_events SET balance = 999999 WHERE seq = 1`)
	s.mu.Unlock()

	_, err := s.ChainIntegrity(hqPub)
	if err == nil {
		t.Fatal("expected chain integrity failure after tampering balance")
	}
}

func TestChainIntegrity_DeletedEvent(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)
	s.AppendSignedEvent(testPK, 500, "earn", "second", hqPriv)
	s.AppendSignedEvent(testPK, -200, "spend", "third", hqPriv)

	// Delete the middle event
	s.mu.Lock()
	s.db.Exec(`DELETE FROM signed_events WHERE seq = 2`)
	s.mu.Unlock()

	_, err := s.ChainIntegrity(hqPub)
	if err == nil {
		t.Fatal("expected chain integrity failure after deleting event")
	}
}

func TestChainIntegrity_WrongKey(t *testing.T) {
	s, _, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "first", hqPriv)

	// Verify with a different key — should fail
	otherPub, _, _ := ed25519.GenerateKey(nil)
	_, err := s.ChainIntegrity(otherPub)
	if err == nil {
		t.Fatal("expected chain integrity failure with wrong HQ key")
	}
}

// ── Counter-signing ─────────────────────────────────────────────────────────

func TestCounterSign(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	ev, _ := s.AppendSignedEvent(testPK, 1000, "grant", "test", hqPriv)

	// Factory counter-signs with its own key
	_, factoryPriv, _ := ed25519.GenerateKey(nil)
	counterSig := signHash(ev.EventHash, factoryPriv)

	if err := s.CounterSign(ev.Seq, counterSig); err != nil {
		t.Fatalf("CounterSign: %v", err)
	}

	// Retrieve and verify both signatures
	events, _ := s.SignedEventsFor(testPK, 10)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].CounterSig == "" {
		t.Error("counter_sig not stored")
	}

	// HQ sig still valid
	if !VerifyEventSig(events[0].EventHash, events[0].HQSig, hqPub) {
		t.Error("HQ sig invalid after counter-sign")
	}
}

// ── Insufficient funds ──────────────────────────────────────────────────────

func TestAppendSignedEvent_InsufficientFunds(t *testing.T) {
	s, _, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 100, "grant", "seed", hqPriv)

	_, err := s.AppendSignedEvent(testPK, -500, "spend", "too much", hqPriv)
	if err == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

// ── Receipt verification ────────────────────────────────────────────────────

func TestReceiptVerification(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	ev, _ := s.AppendSignedEvent(testPK, 1000, "grant", "early adopter", hqPriv)

	// Convert to receipt (what the factory stores locally)
	receipt := ev.ToReceipt(hqPub)

	// Factory can verify offline — no database needed
	if err := VerifyReceipt(&receipt); err != nil {
		t.Fatalf("VerifyReceipt: %v", err)
	}
}

func TestReceiptVerification_Tampered(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	ev, _ := s.AppendSignedEvent(testPK, 1000, "grant", "early adopter", hqPriv)
	receipt := ev.ToReceipt(hqPub)

	// Tamper with amount in receipt
	receipt.Amount = 999999
	if err := VerifyReceipt(&receipt); err == nil {
		t.Fatal("expected receipt verification failure after tampering")
	}
}

func TestReceiptVerification_WrongHQKey(t *testing.T) {
	s, _, hqPriv := tempChainDB(t)

	ev, _ := s.AppendSignedEvent(testPK, 1000, "grant", "early adopter", hqPriv)

	// Use a different HQ public key in the receipt
	otherPub, _, _ := ed25519.GenerateKey(nil)
	receipt := ev.ToReceipt(otherPub)

	if err := VerifyReceipt(&receipt); err == nil {
		t.Fatal("expected receipt verification failure with wrong HQ key")
	}
}

// ── Multiple factories on same chain ────────────────────────────────────────

func TestMultipleFactories(t *testing.T) {
	s, hqPub, hqPriv := tempChainDB(t)

	pk1 := "1111111111111111111111111111111111111111111111111111111111111111"
	pk2 := "2222222222222222222222222222222222222222222222222222222222222222"

	s.AppendSignedEvent(pk1, 1000, "grant", "factory A", hqPriv)
	s.AppendSignedEvent(pk2, 2000, "grant", "factory B", hqPriv)
	s.AppendSignedEvent(pk1, 500, "earn", "job from A", hqPriv)
	s.AppendSignedEvent(pk2, -500, "spend", "build for B", hqPriv)

	// Full chain integrity
	lastSeq, err := s.ChainIntegrity(hqPub)
	if err != nil {
		t.Fatalf("ChainIntegrity: %v", err)
	}
	if lastSeq != 4 {
		t.Errorf("lastSeq = %d, want 4", lastSeq)
	}

	// Per-factory queries
	evA, _ := s.SignedEventsFor(pk1, 10)
	if len(evA) != 2 {
		t.Errorf("factory A events = %d, want 2", len(evA))
	}
	evB, _ := s.SignedEventsFor(pk2, 10)
	if len(evB) != 2 {
		t.Errorf("factory B events = %d, want 2", len(evB))
	}
}

// ── Full chain dump ─────────────────────────────────────────────────────────

func TestFullChain(t *testing.T) {
	s, _, hqPriv := tempChainDB(t)

	s.AppendSignedEvent(testPK, 1000, "grant", "a", hqPriv)
	s.AppendSignedEvent(testPK, 500, "earn", "b", hqPriv)

	chain, err := s.FullChain(100)
	if err != nil {
		t.Fatalf("FullChain: %v", err)
	}
	if len(chain) != 2 {
		t.Errorf("chain length = %d, want 2", len(chain))
	}
	// Should be in ascending order
	if chain[0].Seq > chain[1].Seq {
		t.Error("chain not in ascending order")
	}
}

// ── Persistence across reopen ───────────────────────────────────────────────

func TestChainPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chain-persist.db")

	pub, priv, _ := ed25519.GenerateKey(nil)

	// First session
	s1, _ := Open(path)
	s1.migrateChain()
	s1.AppendSignedEvent(testPK, 1000, "grant", "first session", priv)
	s1.AppendSignedEvent(testPK, 500, "earn", "first session job", priv)
	s1.Close()

	// Second session — chain should survive
	s2, _ := Open(path)
	s2.migrateChain()

	// Append to existing chain
	ev3, err := s2.AppendSignedEvent(testPK, -200, "spend", "second session", priv)
	if err != nil {
		t.Fatalf("append after reopen: %v", err)
	}
	if ev3.Seq != 3 {
		t.Errorf("Seq = %d, want 3", ev3.Seq)
	}

	// Verify full chain including cross-session events
	lastSeq, err := s2.ChainIntegrity(pub)
	if err != nil {
		t.Fatalf("ChainIntegrity after reopen: %v", err)
	}
	if lastSeq != 3 {
		t.Errorf("lastSeq = %d, want 3", lastSeq)
	}
	s2.Close()
}

// ── Empty chain integrity ───────────────────────────────────────────────────

func TestChainIntegrity_Empty(t *testing.T) {
	s, hqPub, _ := tempChainDB(t)

	lastSeq, err := s.ChainIntegrity(hqPub)
	if err != nil {
		t.Fatalf("ChainIntegrity on empty: %v", err)
	}
	if lastSeq != 0 {
		t.Errorf("lastSeq = %d, want 0", lastSeq)
	}
}
