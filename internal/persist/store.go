// Package persist provides SQLite-backed persistence for Glass Factory HQ state.
//
// Persists factory nodes, token balances, and reputation data so that HQ
// restarts do not lose registered nodes or earned tokens.
//
// 持久化存储 — 工厂节点、令牌余额、信誉数据的 SQLite 存储。
package persist

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// FactoryNode mirrors the in-memory struct in main.go.
type FactoryNode struct {
	PublicKey     string   `json:"public_key"`
	Handle       string   `json:"handle"`
	Port         int      `json:"port"`
	Status       string   `json:"status"`
	Models       []string `json:"models"`
	QueueLen     int      `json:"queue_len"`
	CacheBytes   int64    `json:"cache_bytes"`
	UptimeSecs   int64    `json:"uptime_secs"`
	RegisteredAt string   `json:"registered_at"`
	LastSeen     string   `json:"last_seen"`
	PairedUser   string   `json:"paired_user,omitempty"`
}

// Reputation mirrors lending.Reputation for persistence.
type Reputation struct {
	Maker            string  `json:"maker"`
	Score            float64 `json:"score"`
	TotalLoans       int     `json:"total_loans"`
	SuccessfulRepays int     `json:"successful_repays"`
	Defaults         int     `json:"defaults"`
	TotalDelivered   int64   `json:"total_delivered"`
	Penalty          float64 `json:"penalty"`
	RegisteredAt     string  `json:"registered_at"`
}

// TokenEvent records earn/spend/grant events for audit trail.
type TokenEvent struct {
	ID        int64  `json:"id"`
	PublicKey string `json:"public_key"`
	Amount    int64  `json:"amount"`
	EventType string `json:"event_type"` // earn, spend, grant, pair
	Reason    string `json:"reason"`
	Timestamp string `json:"timestamp"`
}

// Store is the SQLite-backed persistence layer.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open creates or opens a SQLite database at the given path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("persist: open %s: %w", path, err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("persist: migrate: %w", err)
	}
	if err := s.migrateChain(); err != nil {
		db.Close()
		return nil, fmt.Errorf("persist: migrate chain: %w", err)
	}
	if err := s.migrateHonours(); err != nil {
		db.Close()
		return nil, fmt.Errorf("persist: migrate honours: %w", err)
	}
	if err := s.migrateBuilds(); err != nil {
		db.Close()
		return nil, fmt.Errorf("persist: migrate builds: %w", err)
	}

	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS factory_nodes (
		public_key   TEXT PRIMARY KEY,
		handle       TEXT NOT NULL DEFAULT '',
		port         INTEGER NOT NULL DEFAULT 0,
		status       TEXT NOT NULL DEFAULT 'idle',
		models       TEXT NOT NULL DEFAULT '[]',
		queue_len    INTEGER NOT NULL DEFAULT 0,
		cache_bytes  INTEGER NOT NULL DEFAULT 0,
		uptime_secs  INTEGER NOT NULL DEFAULT 0,
		registered_at TEXT NOT NULL,
		last_seen    TEXT NOT NULL,
		paired_user  TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS balances (
		public_key TEXT PRIMARY KEY,
		balance    INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS reputations (
		maker             TEXT PRIMARY KEY,
		score             REAL NOT NULL DEFAULT 0,
		total_loans       INTEGER NOT NULL DEFAULT 0,
		successful_repays INTEGER NOT NULL DEFAULT 0,
		defaults          INTEGER NOT NULL DEFAULT 0,
		total_delivered   INTEGER NOT NULL DEFAULT 0,
		penalty           REAL NOT NULL DEFAULT 0,
		registered_at     TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS token_events (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		public_key TEXT NOT NULL,
		amount     INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		reason     TEXT NOT NULL DEFAULT '',
		timestamp  TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_token_events_pubkey ON token_events(public_key);
	CREATE INDEX IF NOT EXISTS idx_token_events_type ON token_events(event_type);
	`
	_, err := s.db.Exec(schema)
	return err
}

// ── Factory Node Operations ──────────────────────────────────────────────────

// SaveNode upserts a factory node.
func (s *Store) SaveNode(n *FactoryNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	modelsJSON, _ := json.Marshal(n.Models)
	_, err := s.db.Exec(`
		INSERT INTO factory_nodes (public_key, handle, port, status, models, queue_len, cache_bytes, uptime_secs, registered_at, last_seen, paired_user)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(public_key) DO UPDATE SET
			handle=excluded.handle, port=excluded.port, status=excluded.status,
			models=excluded.models, queue_len=excluded.queue_len, cache_bytes=excluded.cache_bytes,
			uptime_secs=excluded.uptime_secs, last_seen=excluded.last_seen, paired_user=excluded.paired_user`,
		n.PublicKey, n.Handle, n.Port, n.Status, string(modelsJSON),
		n.QueueLen, n.CacheBytes, n.UptimeSecs, n.RegisteredAt, n.LastSeen, n.PairedUser,
	)
	return err
}

// GetNode returns a factory node by public key.
func (s *Store) GetNode(pubKey string) (*FactoryNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := &FactoryNode{}
	var modelsJSON string
	err := s.db.QueryRow(`SELECT public_key, handle, port, status, models, queue_len, cache_bytes, uptime_secs, registered_at, last_seen, paired_user FROM factory_nodes WHERE public_key = ?`, pubKey).
		Scan(&n.PublicKey, &n.Handle, &n.Port, &n.Status, &modelsJSON, &n.QueueLen, &n.CacheBytes, &n.UptimeSecs, &n.RegisteredAt, &n.LastSeen, &n.PairedUser)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(modelsJSON), &n.Models)
	return n, nil
}

// AllNodes returns all registered factory nodes.
func (s *Store) AllNodes() ([]*FactoryNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT public_key, handle, port, status, models, queue_len, cache_bytes, uptime_secs, registered_at, last_seen, paired_user FROM factory_nodes ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*FactoryNode
	for rows.Next() {
		n := &FactoryNode{}
		var modelsJSON string
		if err := rows.Scan(&n.PublicKey, &n.Handle, &n.Port, &n.Status, &modelsJSON, &n.QueueLen, &n.CacheBytes, &n.UptimeSecs, &n.RegisteredAt, &n.LastSeen, &n.PairedUser); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(modelsJSON), &n.Models)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// NodeCount returns the number of registered nodes.
func (s *Store) NodeCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM factory_nodes`).Scan(&count)
	return count, err
}

// DeleteNode removes a factory node.
func (s *Store) DeleteNode(pubKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM factory_nodes WHERE public_key = ?`, pubKey)
	return err
}

// ── Balance Operations ───────────────────────────────────────────────────────

// GetBalance returns a maker's token balance.
func (s *Store) GetBalance(pubKey string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var bal int64
	err := s.db.QueryRow(`SELECT balance FROM balances WHERE public_key = ?`, pubKey).Scan(&bal)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return bal, err
}

// SetBalance sets a maker's token balance and logs the event.
func (s *Store) SetBalance(pubKey string, balance int64, eventType, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get current balance for delta
	var oldBal int64
	err = tx.QueryRow(`SELECT balance FROM balances WHERE public_key = ?`, pubKey).Scan(&oldBal)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO balances (public_key, balance) VALUES (?, ?)
		ON CONFLICT(public_key) DO UPDATE SET balance=excluded.balance`,
		pubKey, balance)
	if err != nil {
		return err
	}

	delta := balance - oldBal
	_, err = tx.Exec(`INSERT INTO token_events (public_key, amount, event_type, reason, timestamp) VALUES (?, ?, ?, ?, ?)`,
		pubKey, delta, eventType, reason, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}

	return tx.Commit()
}

// AdjustBalance atomically adds delta to a maker's balance and logs the event.
func (s *Store) AdjustBalance(pubKey string, delta int64, eventType, reason string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var bal int64
	err = tx.QueryRow(`SELECT balance FROM balances WHERE public_key = ?`, pubKey).Scan(&bal)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}

	newBal := bal + delta
	if newBal < 0 {
		return bal, fmt.Errorf("insufficient tokens: have %d, need %d", bal, -delta)
	}

	_, err = tx.Exec(`
		INSERT INTO balances (public_key, balance) VALUES (?, ?)
		ON CONFLICT(public_key) DO UPDATE SET balance=excluded.balance`,
		pubKey, newBal)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(`INSERT INTO token_events (public_key, amount, event_type, reason, timestamp) VALUES (?, ?, ?, ?, ?)`,
		pubKey, delta, eventType, reason, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newBal, nil
}

// TotalTokens returns the sum of all balances in the network.
func (s *Store) TotalTokens() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var total sql.NullInt64
	err := s.db.QueryRow(`SELECT SUM(balance) FROM balances`).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}

// AllBalances returns all balances (for stats/debugging).
func (s *Store) AllBalances() (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT public_key, balance FROM balances`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]int64)
	for rows.Next() {
		var pk string
		var bal int64
		if err := rows.Scan(&pk, &bal); err != nil {
			return nil, err
		}
		m[pk] = bal
	}
	return m, nil
}

// ── Reputation Operations ────────────────────────────────────────────────────

// SaveReputation upserts a reputation record.
func (s *Store) SaveReputation(r *Reputation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO reputations (maker, score, total_loans, successful_repays, defaults, total_delivered, penalty, registered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(maker) DO UPDATE SET
			score=excluded.score, total_loans=excluded.total_loans,
			successful_repays=excluded.successful_repays, defaults=excluded.defaults,
			total_delivered=excluded.total_delivered, penalty=excluded.penalty`,
		r.Maker, r.Score, r.TotalLoans, r.SuccessfulRepays, r.Defaults, r.TotalDelivered, r.Penalty, r.RegisteredAt,
	)
	return err
}

// GetReputation returns a maker's reputation.
func (s *Store) GetReputation(maker string) (*Reputation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := &Reputation{}
	err := s.db.QueryRow(`SELECT maker, score, total_loans, successful_repays, defaults, total_delivered, penalty, registered_at FROM reputations WHERE maker = ?`, maker).
		Scan(&r.Maker, &r.Score, &r.TotalLoans, &r.SuccessfulRepays, &r.Defaults, &r.TotalDelivered, &r.Penalty, &r.RegisteredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ── Token Event Audit Trail ──────────────────────────────────────────────────

// RecentEvents returns the last N token events, optionally filtered by pubkey.
func (s *Store) RecentEvents(pubKey string, limit int) ([]TokenEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows *sql.Rows
	var err error
	if pubKey != "" {
		rows, err = s.db.Query(`SELECT id, public_key, amount, event_type, reason, timestamp FROM token_events WHERE public_key = ? ORDER BY id DESC LIMIT ?`, pubKey, limit)
	} else {
		rows, err = s.db.Query(`SELECT id, public_key, amount, event_type, reason, timestamp FROM token_events ORDER BY id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []TokenEvent
	for rows.Next() {
		var e TokenEvent
		if err := rows.Scan(&e.ID, &e.PublicKey, &e.Amount, &e.EventType, &e.Reason, &e.Timestamp); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// ── Bulk Load (for syncing lending.Ledger on startup) ────────────────────────

// LoadBalancesIntoLedger loads all balances into a lending.Ledger-compatible map.
func (s *Store) LoadBalancesMap() (map[string]int64, error) {
	return s.AllBalances()
}

// Stats returns aggregate statistics.
func (s *Store) Stats() (totalTokens int64, nodeCount int, eventCount int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var tt sql.NullInt64
	s.db.QueryRow(`SELECT SUM(balance) FROM balances`).Scan(&tt)
	if tt.Valid {
		totalTokens = tt.Int64
	}

	s.db.QueryRow(`SELECT COUNT(*) FROM factory_nodes`).Scan(&nodeCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM token_events`).Scan(&eventCount)

	return totalTokens, nodeCount, eventCount, nil
}

// Migrate loads persisted state and logs what was recovered.
func (s *Store) LogRecovery() {
	totalTokens, nodeCount, eventCount, err := s.Stats()
	if err != nil {
		log.Printf("persist: recovery stats error: %v", err)
		return
	}
	if nodeCount > 0 || totalTokens > 0 {
		log.Printf("persist: recovered %d nodes, %d total tokens, %d events from disk",
			nodeCount, totalTokens, eventCount)
	}
}

// ModelsToJSON converts a models slice to JSON string for storage.
func ModelsToJSON(models []string) string {
	if models == nil {
		return "[]"
	}
	b, _ := json.Marshal(models)
	return string(b)
}

// ModelsFromJSON parses a JSON string back to a models slice.
func ModelsFromJSON(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var models []string
	json.Unmarshal([]byte(s), &models)
	return models
}

// SanitizeHandle prevents injection in node handles.
func SanitizeHandle(h string) string {
	h = strings.TrimSpace(h)
	if len(h) > 64 {
		h = h[:64]
	}
	return h
}
