// Package knowledge implements the factory learning federation protocol.
//
// Every factory extracts patterns, lessons, and failure modes from its
// forge runs. Knowledge classified as "contribute" is shared with the
// network. Confidence is validated through real-world use — good
// patterns survive, bad ones get pruned.
//
// 工厂学习协议 — 每个工厂都从每次运行中学习，网络将所有参与者的学习复合在一起。
package knowledge

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is a unit of factory knowledge extracted from a real forge job.
type Entry struct {
	ID             string  `json:"id"`
	Category       string  `json:"category"`        // pattern, lesson, failure_mode, proof_strategy, test_strategy, architecture_decision, ada_knowledge
	Topic          string  `json:"topic"`
	Content        string  `json:"content"`
	Language       string  `json:"language"`         // go, ada, rust, python, etc.
	Confidence     float64 `json:"confidence"`       // 0.0–1.0, adjusted through use
	UsedCount      int     `json:"used_count"`
	SourceJob      string  `json:"source_job"`
	SourceFactory  string  `json:"source_factory"`
	Contributors   int     `json:"contributors"`     // number of factories that confirmed this
	ProofChainHash string  `json:"proof_chain_hash"` // SHA-256 of the stage proof chain
	SignerPubKey   string  `json:"signer_pubkey"`
	Signature      string  `json:"signature"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// Store is the persistence interface for knowledge entries.
type Store interface {
	Save(e *Entry) error
	Get(id string) (*Entry, error)
	Query(category, language string, topics []string, limit int) ([]Entry, error)
	UpdateConfidence(id string, delta float64) error
	Prune(minConfidence float64) (int, error)
	Count() int
}

// ── In-Memory Store (reference implementation) ──────────────────────────────

// MemStore is a simple in-memory knowledge store for the reference implementation.
type MemStore struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

func NewMemStore() *MemStore {
	return &MemStore{entries: make(map[string]*Entry)}
}

func (s *MemStore) Save(e *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Merge if same topic+category+language from different factory
	for _, existing := range s.entries {
		if existing.Topic == e.Topic && existing.Category == e.Category && existing.Language == e.Language && existing.SourceFactory != e.SourceFactory {
			existing.Contributors++
			if e.Confidence > existing.Confidence {
				existing.Confidence = e.Confidence
			}
			existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return nil
		}
	}

	e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.entries[e.ID] = e
	return nil
}

func (s *MemStore) Get(id string) (*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, fmt.Errorf("knowledge entry %s not found", id)
	}
	return e, nil
}

func (s *MemStore) Query(category, language string, topics []string, limit int) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Entry
	for _, e := range s.entries {
		if category != "" && e.Category != category {
			continue
		}
		if language != "" && e.Language != language {
			continue
		}
		if len(topics) > 0 && !matchesTopics(e.Topic, topics) {
			continue
		}
		results = append(results, *e)
	}

	// Sort by confidence * contributors (network-validated knowledge ranks higher)
	sort.Slice(results, func(i, j int) bool {
		scoreI := results[i].Confidence * float64(max(1, results[i].Contributors))
		scoreJ := results[j].Confidence * float64(max(1, results[j].Contributors))
		return scoreI > scoreJ
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *MemStore) UpdateConfidence(id string, delta float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return fmt.Errorf("entry %s not found", id)
	}
	e.Confidence += delta
	if e.Confidence > 1.0 {
		e.Confidence = 1.0
	}
	if e.Confidence < 0.0 {
		e.Confidence = 0.0
	}
	e.UsedCount++
	e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

func (s *MemStore) Prune(minConfidence float64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pruned := 0
	for id, e := range s.entries {
		if e.Confidence < minConfidence {
			delete(s.entries, id)
			pruned++
		}
	}
	return pruned, nil
}

func (s *MemStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func matchesTopics(topic string, queries []string) bool {
	lower := strings.ToLower(topic)
	for _, q := range queries {
		if strings.Contains(lower, strings.ToLower(q)) {
			return true
		}
	}
	return false
}

// ── Signing ─────────────────────────────────────────────────────────────────

// SignEntry signs a knowledge entry with a factory's Ed25519 key.
func SignEntry(e *Entry, privKey ed25519.PrivateKey, pubKey ed25519.PublicKey) {
	e.SignerPubKey = hex.EncodeToString(pubKey)
	msg := entrySigningPayload(e)
	sig := ed25519.Sign(privKey, msg)
	e.Signature = hex.EncodeToString(sig)
}

// VerifyEntry checks the Ed25519 signature on a knowledge entry.
func VerifyEntry(e *Entry) (bool, error) {
	pubBytes, err := hex.DecodeString(e.SignerPubKey)
	if err != nil {
		return false, fmt.Errorf("decode pubkey: %w", err)
	}
	sigBytes, err := hex.DecodeString(e.Signature)
	if err != nil {
		return false, fmt.Errorf("decode signature: %w", err)
	}
	msg := entrySigningPayload(e)
	return ed25519.Verify(ed25519.PublicKey(pubBytes), msg, sigBytes), nil
}

func entrySigningPayload(e *Entry) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s",
		e.ID, e.Category, e.Topic, e.Content, e.Language, e.SourceJob, e.SourceFactory, e.ProofChainHash))
}

// HashProofChain computes SHA-256 of a proof chain JSON for knowledge provenance.
func HashProofChain(proofChainJSON string) string {
	h := sha256.Sum256([]byte(proofChainJSON))
	return hex.EncodeToString(h[:])
}

// ── Contribution Validation ─────────────────────────────────────────────────

// ValidateContribution checks that a knowledge contribution is legitimate.
func ValidateContribution(e *Entry) error {
	if e.ID == "" {
		return fmt.Errorf("missing ID")
	}
	if e.Category == "" {
		return fmt.Errorf("missing category")
	}
	validCategories := map[string]bool{
		"pattern": true, "lesson": true, "failure_mode": true,
		"proof_strategy": true, "test_strategy": true,
		"architecture_decision": true, "ada_knowledge": true,
	}
	if !validCategories[e.Category] {
		return fmt.Errorf("invalid category: %s", e.Category)
	}
	if e.Topic == "" || e.Content == "" {
		return fmt.Errorf("missing topic or content")
	}
	if e.ProofChainHash == "" {
		return fmt.Errorf("missing proof chain hash — knowledge must come from real work")
	}
	if e.Signature == "" || e.SignerPubKey == "" {
		return fmt.Errorf("unsigned contribution")
	}
	valid, err := VerifyEntry(e)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

// ── Federation ──────────────────────────────────────────────────────────────

// ContributeRequest is the wire format for POST /api/knowledge/contribute.
type ContributeRequest struct {
	Entries []Entry `json:"entries"`
}

// ContributeResponse is the wire format for the response.
type ContributeResponse struct {
	Accepted        int     `json:"accepted"`
	Rejected        int     `json:"rejected"`
	ReputationDelta float64 `json:"reputation_delta"`
	Errors          []string `json:"errors,omitempty"`
}

// QueryRequest is the wire format for POST /api/knowledge/query.
type QueryRequest struct {
	Category string   `json:"category"`
	Language string   `json:"language"`
	Topics   []string `json:"topics"`
	Limit    int      `json:"limit"`
}

// MarshalForAgent formats knowledge entries for injection into agent prompts.
func MarshalForAgent(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Factory Knowledge (from network)\n\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("### %s [%s] (confidence: %.0f%%, confirmed by %d factories)\n",
			e.Topic, e.Category, e.Confidence*100, e.Contributors))
		b.WriteString(e.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Marshal/unmarshal helpers for wire format.
func MarshalContributeRequest(r *ContributeRequest) ([]byte, error) {
	return json.Marshal(r)
}

func UnmarshalContributeRequest(data []byte) (*ContributeRequest, error) {
	var r ContributeRequest
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
