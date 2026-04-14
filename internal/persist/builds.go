// builds.go persists build submissions and their lifecycle.
//
// 构建任务 — 从规格说明提交到完成交付的全生命周期。
package persist

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// BuildStatus tracks the lifecycle of a build.
type BuildStatus string

const (
	BuildQueued    BuildStatus = "queued"
	BuildAssigned  BuildStatus = "assigned"
	BuildRunning   BuildStatus = "running"
	BuildVerifying BuildStatus = "verifying"
	BuildComplete  BuildStatus = "complete"
	BuildFailed    BuildStatus = "failed"
)

// Build is a spec submission queued for the network.
type Build struct {
	ID          string      `json:"id"`           // 16-char hex
	PublicKey   string      `json:"public_key"`   // submitter (empty for anonymous)
	Spec        string      `json:"spec"`
	Destination string      `json:"destination"`  // local, network, company
	Status      BuildStatus `json:"status"`
	Cost        int64       `json:"cost"`         // ◎ charged
	AssignedTo  string      `json:"assigned_to"`  // factory node pubkey
	Language    string      `json:"language"`      // detected or specified
	SubmittedAt string      `json:"submitted_at"`
	UpdatedAt   string      `json:"updated_at"`
	Result      string      `json:"result"`       // build output or error
}

// migrateBuilds creates the builds table.
func (s *Store) migrateBuilds() error {
	schema := `
	CREATE TABLE IF NOT EXISTS builds (
		id           TEXT PRIMARY KEY,
		public_key   TEXT NOT NULL DEFAULT '',
		spec         TEXT NOT NULL,
		destination  TEXT NOT NULL DEFAULT 'network',
		status       TEXT NOT NULL DEFAULT 'queued',
		cost         INTEGER NOT NULL DEFAULT 0,
		assigned_to  TEXT NOT NULL DEFAULT '',
		language     TEXT NOT NULL DEFAULT 'go',
		submitted_at TEXT NOT NULL,
		updated_at   TEXT NOT NULL,
		result       TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_builds_status ON builds(status);
	CREATE INDEX IF NOT EXISTS idx_builds_pubkey ON builds(public_key);
	CREATE INDEX IF NOT EXISTS idx_builds_assigned ON builds(assigned_to);
	`
	_, err := s.db.Exec(schema)
	return err
}

// GenerateBuildID creates a random 16-character hex ID.
func GenerateBuildID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// EstimateCost returns the ◎ cost for a spec based on length and complexity.
// Simple heuristic: base cost + per-character cost.
func EstimateCost(spec string) int64 {
	base := int64(10) // minimum ◎10 per build
	chars := int64(len(spec))

	// Rough tiers:
	// < 500 chars: simple spec → ◎10-15
	// 500-2000 chars: medium spec → ◎15-30
	// 2000+ chars: complex spec → ◎30+
	perChar := int64(1) // 1 ◎ per 100 chars
	charCost := chars / 100 * perChar

	total := base + charCost
	if total > 200 {
		total = 200 // cap at ◎200 per build
	}
	return total
}

// DetectLanguage tries to detect the target language from the spec text.
func DetectLanguage(spec string) string {
	lower := []byte(spec)
	// Simple keyword detection
	for i := range lower {
		if lower[i] >= 'A' && lower[i] <= 'Z' {
			lower[i] = lower[i] + 32
		}
	}
	s := string(lower)
	if contains(s, "ada") || contains(s, "spark") || contains(s, "gnat") {
		return "ada"
	}
	return "go" // default
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── Build Operations ─────────────────────────────────────────────────────────

// SubmitBuild creates a new queued build.
func (s *Store) SubmitBuild(b *Build) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO builds (id, public_key, spec, destination, status, cost, assigned_to, language, submitted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.PublicKey, b.Spec, b.Destination, b.Status, b.Cost,
		b.AssignedTo, b.Language, b.SubmittedAt, b.UpdatedAt,
	)
	return err
}

// GetBuild returns a build by ID.
func (s *Store) GetBuild(id string) (*Build, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := &Build{}
	err := s.db.QueryRow(
		`SELECT id, public_key, spec, destination, status, cost, assigned_to, language, submitted_at, updated_at, result
		 FROM builds WHERE id = ?`, id,
	).Scan(&b.ID, &b.PublicKey, &b.Spec, &b.Destination, &b.Status, &b.Cost,
		&b.AssignedTo, &b.Language, &b.SubmittedAt, &b.UpdatedAt, &b.Result)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// UpdateBuildStatus updates a build's status and result.
func (s *Store) UpdateBuildStatus(id string, status BuildStatus, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`UPDATE builds SET status = ?, result = ?, updated_at = ? WHERE id = ?`,
		status, result, time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

// AssignBuild assigns a queued build to a factory node.
func (s *Store) AssignBuild(id, factoryPubKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`UPDATE builds SET status = 'assigned', assigned_to = ?, updated_at = ? WHERE id = ? AND status = 'queued'`,
		factoryPubKey, time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

// NextQueuedBuild returns the oldest queued build for a factory to pick up.
func (s *Store) NextQueuedBuild() (*Build, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := &Build{}
	err := s.db.QueryRow(
		`SELECT id, public_key, spec, destination, status, cost, assigned_to, language, submitted_at, updated_at, result
		 FROM builds WHERE status = 'queued' ORDER BY submitted_at ASC LIMIT 1`,
	).Scan(&b.ID, &b.PublicKey, &b.Spec, &b.Destination, &b.Status, &b.Cost,
		&b.AssignedTo, &b.Language, &b.SubmittedAt, &b.UpdatedAt, &b.Result)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// QueuedBuilds returns all queued builds.
func (s *Store) QueuedBuilds() ([]*Build, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, public_key, spec, destination, status, cost, assigned_to, language, submitted_at, updated_at, result
		 FROM builds WHERE status = 'queued' ORDER BY submitted_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []*Build
	for rows.Next() {
		b := &Build{}
		if err := rows.Scan(&b.ID, &b.PublicKey, &b.Spec, &b.Destination, &b.Status, &b.Cost,
			&b.AssignedTo, &b.Language, &b.SubmittedAt, &b.UpdatedAt, &b.Result); err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, nil
}

// BuildsBySubmitter returns builds for a specific submitter.
func (s *Store) BuildsBySubmitter(pubKey string, limit int) ([]*Build, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, public_key, spec, destination, status, cost, assigned_to, language, submitted_at, updated_at, result
		 FROM builds WHERE public_key = ? ORDER BY submitted_at DESC LIMIT ?`, pubKey, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []*Build
	for rows.Next() {
		b := &Build{}
		if err := rows.Scan(&b.ID, &b.PublicKey, &b.Spec, &b.Destination, &b.Status, &b.Cost,
			&b.AssignedTo, &b.Language, &b.SubmittedAt, &b.UpdatedAt, &b.Result); err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, nil
}

// BuildStats returns counts by status.
func (s *Store) BuildStats() (queued, running, complete, failed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.db.QueryRow(`SELECT COUNT(*) FROM builds WHERE status = 'queued'`).Scan(&queued)
	s.db.QueryRow(`SELECT COUNT(*) FROM builds WHERE status IN ('assigned','running','verifying')`).Scan(&running)
	s.db.QueryRow(`SELECT COUNT(*) FROM builds WHERE status = 'complete'`).Scan(&complete)
	s.db.QueryRow(`SELECT COUNT(*) FROM builds WHERE status = 'failed'`).Scan(&failed)
	return queued, running, complete, failed, nil
}

// PurgeBuild removes a build (admin use).
func (s *Store) PurgeBuild(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM builds WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("build %s not found", id)
	}
	return nil
}
