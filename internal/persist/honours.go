// honours.go persists the King's honours (Knights and Ministers) and audience records.
//
// 荣誉与觐见 — 骑士、大臣的册封记录和与国王的对话记录。
package persist

import (
	"database/sql"
	"time"
)

// Honour is a King-granted title.
type Honour struct {
	PublicKey string `json:"public_key"`
	Rank      string `json:"rank"`      // knight, minister
	KingName  string `json:"king_name"` // name chosen by the King (final)
	Nickname  string `json:"nickname"`  // short name in any language
	GrantedAt string `json:"granted_at"`
	Reason    string `json:"reason"`
}

// Audience is a record of someone speaking with the King.
type Audience struct {
	ID          int64  `json:"id"`
	PublicKey   string `json:"public_key"`
	Message     string `json:"message"`
	MessageEN   string `json:"message_en,omitempty"`  // English translation of petition
	MessageZH   string `json:"message_zh,omitempty"`  // Chinese translation of petition
	Response    string `json:"response"`
	Tone        string `json:"tone"`                  // polite, sharp, roast, commendation, cool
	Visibility  string `json:"visibility"`            // public, private — decided by the King
	Nickname    string `json:"nickname,omitempty"`     // display name for public view
	Timestamp   string `json:"timestamp"`
}

// migrateHonours creates the honours and audiences tables.
func (s *Store) migrateHonours() error {
	schema := `
	CREATE TABLE IF NOT EXISTS honours (
		public_key TEXT PRIMARY KEY,
		rank       TEXT NOT NULL DEFAULT 'subject',
		king_name  TEXT NOT NULL DEFAULT '',
		nickname   TEXT NOT NULL DEFAULT '',
		granted_at TEXT NOT NULL,
		reason     TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS audiences (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		public_key TEXT NOT NULL DEFAULT '',
		message    TEXT NOT NULL,
		response   TEXT NOT NULL DEFAULT '',
		tone       TEXT NOT NULL DEFAULT 'polite',
		timestamp  TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_audiences_pubkey ON audiences(public_key);
	CREATE INDEX IF NOT EXISTS idx_audiences_time ON audiences(timestamp);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add visibility, nickname, and translation columns if missing
	s.db.Exec(`ALTER TABLE audiences ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'`)
	s.db.Exec(`ALTER TABLE audiences ADD COLUMN nickname TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE audiences ADD COLUMN message_en TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`ALTER TABLE audiences ADD COLUMN message_zh TEXT NOT NULL DEFAULT ''`)
	s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_audiences_visibility ON audiences(visibility)`)

	return nil
}

// ── Honour Operations ────────────────────────────────────────────────────────

// GrantHonour upserts an honour record.
func (s *Store) GrantHonour(h *Honour) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO honours (public_key, rank, king_name, nickname, granted_at, reason)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(public_key) DO UPDATE SET
			rank=excluded.rank, king_name=excluded.king_name,
			nickname=excluded.nickname, reason=excluded.reason`,
		h.PublicKey, h.Rank, h.KingName, h.Nickname, h.GrantedAt, h.Reason,
	)
	return err
}

// GetHonour returns an honour by public key, or nil if none granted.
func (s *Store) GetHonour(pubKey string) (*Honour, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := &Honour{}
	err := s.db.QueryRow(
		`SELECT public_key, rank, king_name, nickname, granted_at, reason FROM honours WHERE public_key = ?`,
		pubKey,
	).Scan(&h.PublicKey, &h.Rank, &h.KingName, &h.Nickname, &h.GrantedAt, &h.Reason)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}

// AllHonours returns all honours, ordered by grant date descending.
func (s *Store) AllHonours() ([]*Honour, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT public_key, rank, king_name, nickname, granted_at, reason FROM honours ORDER BY granted_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var honours []*Honour
	for rows.Next() {
		h := &Honour{}
		if err := rows.Scan(&h.PublicKey, &h.Rank, &h.KingName, &h.Nickname, &h.GrantedAt, &h.Reason); err != nil {
			return nil, err
		}
		honours = append(honours, h)
	}
	return honours, nil
}

// HonourCount returns the number of honoured subjects by rank.
func (s *Store) HonourCount(rank string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM honours WHERE rank = ?`, rank).Scan(&count)
	return count, err
}

// SetNickname updates an honour's nickname (the holder's choice).
func (s *Store) SetNickname(pubKey, nickname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE honours SET nickname = ? WHERE public_key = ?`, nickname, pubKey)
	return err
}

// ── Audience Operations ──────────────────────────────────────────────────────

// RecordAudience saves a conversation with the King.
func (s *Store) RecordAudience(a *Audience) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a.Visibility == "" {
		a.Visibility = "private"
	}

	result, err := s.db.Exec(`
		INSERT INTO audiences (public_key, message, message_en, message_zh, response, tone, visibility, nickname, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.PublicKey, a.Message, a.MessageEN, a.MessageZH, a.Response, a.Tone, a.Visibility, a.Nickname, a.Timestamp,
	)
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	a.ID = id
	return nil
}

// RecentAudiences returns the last N audiences, optionally filtered by pubkey.
// This returns ALL audiences regardless of visibility — use for internal/mother's view.
func (s *Store) RecentAudiences(pubKey string, limit int) ([]*Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows *sql.Rows
	var err error
	if pubKey != "" {
		rows, err = s.db.Query(
			`SELECT id, public_key, message, message_en, message_zh, response, tone, visibility, nickname, timestamp FROM audiences WHERE public_key = ? ORDER BY id DESC LIMIT ?`,
			pubKey, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, public_key, message, message_en, message_zh, response, tone, visibility, nickname, timestamp FROM audiences ORDER BY id DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var audiences []*Audience
	for rows.Next() {
		a := &Audience{}
		if err := rows.Scan(&a.ID, &a.PublicKey, &a.Message, &a.MessageEN, &a.MessageZH, &a.Response, &a.Tone, &a.Visibility, &a.Nickname, &a.Timestamp); err != nil {
			return nil, err
		}
		audiences = append(audiences, a)
	}
	return audiences, nil
}

// PublicAudiences returns the last N audiences that the King marked as public.
// This is the petition wall — what the community sees.
// 公开觐见 — 大王决定哪些对话公开。
func (s *Store) PublicAudiences(limit int) ([]*Audience, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, public_key, message, message_en, message_zh, response, tone, visibility, nickname, timestamp FROM audiences WHERE visibility = 'public' ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var audiences []*Audience
	for rows.Next() {
		a := &Audience{}
		if err := rows.Scan(&a.ID, &a.PublicKey, &a.Message, &a.MessageEN, &a.MessageZH, &a.Response, &a.Tone, &a.Visibility, &a.Nickname, &a.Timestamp); err != nil {
			return nil, err
		}
		audiences = append(audiences, a)
	}
	return audiences, nil
}

// AudienceCount returns the number of audiences for a pubkey, or total if empty.
func (s *Store) AudienceCount(pubKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	var err error
	if pubKey != "" {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM audiences WHERE public_key = ?`, pubKey).Scan(&count)
	} else {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM audiences`).Scan(&count)
	}
	return count, err
}

// EarnedTokens returns the total earned and spent for a pubkey from token events.
func (s *Store) EarnedTokens(pubKey string) (earned int64, spent int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.db.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM token_events WHERE public_key = ? AND amount > 0`, pubKey,
	).Scan(&earned)
	s.db.QueryRow(
		`SELECT COALESCE(SUM(ABS(amount)), 0) FROM token_events WHERE public_key = ? AND amount < 0`, pubKey,
	).Scan(&spent)
	return earned, spent, nil
}

// BuildsCompleted returns the number of 'earn' events for a pubkey (proxy for builds).
func (s *Store) BuildsCompleted(pubKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM token_events WHERE public_key = ? AND event_type = 'earn'`, pubKey,
	).Scan(&count)
	return count, err
}

// LastAudienceTime returns the timestamp of the most recent audience for rate limiting.
func (s *Store) LastAudienceTime(pubKey string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ts string
	err := s.db.QueryRow(
		`SELECT timestamp FROM audiences WHERE public_key = ? ORDER BY id DESC LIMIT 1`, pubKey,
	).Scan(&ts)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	t, _ := time.Parse(time.RFC3339, ts)
	return t, nil
}
