// glassfactory is the standalone Glass Factory registry server.
// Serves the component registry, search, federation, and health endpoints.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"glassfactory/internal/knowledge"
	"glassfactory/internal/lending"
	"glassfactory/internal/persist"
)

// ── Inline registry (extracted from forge/components/universal.go) ───────────

type ComponentDescriptor struct {
	UID            string            `json:"uid"`
	DisplayName    string            `json:"display_name"`
	DisplayNameZH  string            `json:"display_name_zh,omitempty"`
	Description    string            `json:"description"`
	DescriptionZH  string            `json:"description_zh,omitempty"`
	Version        string            `json:"version"`
	Translations   map[string]Translation `json:"translations,omitempty"`
	Capabilities   []string          `json:"capabilities"`
	Patterns       []string          `json:"patterns"`
	Interfaces     []string          `json:"interfaces"`
	Concerns       []string          `json:"concerns"`
	Implementations []Implementation `json:"implementations"`
	History        []HistoryEntry    `json:"history"`
	AttrHash       string            `json:"attr_hash"`
	SourceRegistry string            `json:"source_registry,omitempty"`
}

type Translation struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

type Implementation struct {
	Language     string   `json:"language"`
	Version      string  `json:"version"`
	PackagePath  string  `json:"package_path"`
	Files        []string `json:"files"`
	Dependencies []string `json:"dependencies"`
}

type HistoryEntry struct {
	Hash      string `json:"hash"`
	PrevHash  string `json:"prev_hash"`
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Detail    string `json:"detail"`
	Signature string `json:"signature,omitempty"`
}

type PeerRegistry struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Trusted  bool   `json:"trusted"`
	LastSeen string `json:"last_seen"`
}

type SearchResult struct {
	Component *ComponentDescriptor `json:"component"`
	Score     float64              `json:"score"`
	Source    string               `json:"source"`
}

// Registry holds all components in memory.
type Registry struct {
	mu         sync.RWMutex
	components map[string]*ComponentDescriptor
	peers      []PeerRegistry
	factoryID  string
}

func NewRegistry(factoryID string) *Registry {
	return &Registry{
		components: make(map[string]*ComponentDescriptor),
		factoryID:  factoryID,
	}
}

func (r *Registry) Register(desc *ComponentDescriptor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.components[desc.UID] = desc
}

func (r *Registry) Get(uid string) (*ComponentDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.components[uid]
	return d, ok
}

func (r *Registry) All() []*ComponentDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*ComponentDescriptor
	for _, d := range r.components {
		all = append(all, d)
	}
	return all
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.components)
}

func (r *Registry) Search(caps, pats, ifcs, cons []string) []SearchResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []SearchResult
	for _, desc := range r.components {
		score := scoreMatch(desc, caps, pats, ifcs, cons)
		if score > 0 {
			results = append(results, SearchResult{Component: desc, Score: score, Source: "local"})
		}
	}
	return results
}

func (r *Registry) AddPeer(p PeerRegistry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers = append(r.peers, p)
}

func (r *Registry) Peers() []PeerRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PeerRegistry, len(r.peers))
	copy(out, r.peers)
	return out
}

func scoreMatch(desc *ComponentDescriptor, caps, pats, ifcs, cons []string) float64 {
	total := len(caps) + len(pats) + len(ifcs) + len(cons)
	if total == 0 {
		return 0
	}
	hits := overlap(desc.Capabilities, caps) + overlap(desc.Patterns, pats) +
		overlap(desc.Interfaces, ifcs) + overlap(desc.Concerns, cons)
	return float64(hits) / float64(total)
}

func overlap(have, want []string) int {
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[strings.ToLower(h)] = true
	}
	n := 0
	for _, w := range want {
		if set[strings.ToLower(w)] {
			n++
		}
	}
	return n
}

// ── HTTP Handlers ───────────────────────────────────────────────────────────

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	factoryID := os.Getenv("FACTORY_ID")
	if factoryID == "" {
		factoryID = "https://thedarkfactory.dev"
	}

	dbPath := os.Getenv("HQ_DB_PATH")
	if dbPath == "" {
		dbPath = "glassfactory.db"
	}

	store, err := persist.Open(dbPath)
	if err != nil {
		log.Fatalf("persist: %v", err)
	}
	defer store.Close()
	store.LogRecovery()

	reg := NewRegistry(factoryID)
	knowledgeStore := knowledge.NewMemStore()
	ledger := lending.NewLedger()

	// Sync persisted balances into lending ledger on startup
	if bals, err := store.LoadBalancesMap(); err == nil {
		for pk, bal := range bals {
			ledger.RegisterMaker(pk)
			ledger.SetBalance(pk, bal)
		}
		if len(bals) > 0 {
			log.Printf("glassfactory: synced %d balances into ledger from disk", len(bals))
		}
	}

	// Seed with the basic-sentinel component
	reg.Register(&ComponentDescriptor{
		UID:           "basic-sentinel",
		DisplayName:   "Basic Sentinel",
		DisplayNameZH: "基础哨兵",
		Description:   "Config-driven health monitor and remediation agent with Ed25519 signed protocol",
		DescriptionZH: "配置驱动的健康监控和修复代理，使用Ed25519签名协议",
		Version:       "1.0.0",
		Capabilities:  []string{"monitors-health", "restarts-services", "alerts-humans"},
		Patterns:      []string{"supervisor", "fixer", "observer"},
		Interfaces:    []string{"health-checker", "remediation-agent"},
		Concerns:      []string{"reliability", "observability"},
		Implementations: []Implementation{{
			Language:    "go",
			Version:     "1.0.0",
			PackagePath: "github.com/glass-factory/basic-sentinel",
		}},
		SourceRegistry: factoryID,
	})

	mux := http.NewServeMux()

	// Registry endpoints
	mux.HandleFunc("GET /api/registry/health", func(w http.ResponseWriter, r *http.Request) {
		nodeCount, _ := store.NodeCount()
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"registry":   "glass-factory",
			"factory_id": factoryID,
			"version":    "0.2.0",
			"components": reg.Count(),
			"peers":      len(reg.Peers()),
			"knowledge":  knowledgeStore.Count(),
			"factories":  nodeCount,
			"protocol":   "0.1",
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /api/registry/search", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Capabilities []string `json:"capabilities"`
			Patterns     []string `json:"patterns"`
			Interfaces   []string `json:"interfaces"`
			Concerns     []string `json:"concerns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		results := reg.Search(req.Capabilities, req.Patterns, req.Interfaces, req.Concerns)
		json.NewEncoder(w).Encode(results)
	})

	mux.HandleFunc("GET /api/registry/components", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(reg.All())
	})

	mux.HandleFunc("GET /api/registry/component/", func(w http.ResponseWriter, r *http.Request) {
		uid := strings.TrimPrefix(r.URL.Path, "/api/registry/component/")
		if uid == "" {
			http.Error(w, `{"error":"uid required"}`, http.StatusBadRequest)
			return
		}
		desc, ok := reg.Get(uid)
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(desc)
	})

	mux.HandleFunc("POST /api/registry/components", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var desc ComponentDescriptor
		if err := json.Unmarshal(body, &desc); err != nil {
			http.Error(w, `{"error":"invalid component"}`, http.StatusBadRequest)
			return
		}
		if desc.UID == "" {
			http.Error(w, `{"error":"uid required"}`, http.StatusBadRequest)
			return
		}
		desc.SourceRegistry = factoryID
		reg.Register(&desc)
		log.Printf("registered component: %s", desc.UID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"uid": desc.UID, "status": "registered"})
	})

	mux.HandleFunc("GET /api/registry/peers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(reg.Peers())
	})

	// Knowledge endpoints
	mux.HandleFunc("POST /api/knowledge/contribute", func(w http.ResponseWriter, r *http.Request) {
		var req knowledge.ContributeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		accepted, rejected := 0, 0
		var errors []string
		for i := range req.Entries {
			if err := knowledge.ValidateContribution(&req.Entries[i]); err != nil {
				rejected++
				errors = append(errors, err.Error())
				continue
			}
			knowledgeStore.Save(&req.Entries[i])
			accepted++
		}
		json.NewEncoder(w).Encode(knowledge.ContributeResponse{
			Accepted:        accepted,
			Rejected:        rejected,
			ReputationDelta: float64(accepted) * 0.005,
			Errors:          errors,
		})
	})

	mux.HandleFunc("POST /api/knowledge/query", func(w http.ResponseWriter, r *http.Request) {
		var req knowledge.QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		results, _ := knowledgeStore.Query(req.Category, req.Language, req.Topics, req.Limit)
		json.NewEncoder(w).Encode(results)
	})

	// Lending endpoints
	mux.HandleFunc("POST /api/tokens/lend", func(w http.ResponseWriter, r *http.Request) {
		var offer lending.Offer
		if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if err := ledger.Lend(&offer); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"offer_id": offer.ID, "status": "available"})
	})

	mux.HandleFunc("POST /api/tokens/borrow", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			LoanID   string `json:"loan_id"`
			OfferID  string `json:"offer_id"`
			Borrower string `json:"borrower"`
			Purpose  string `json:"purpose"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		loan, err := ledger.Borrow(req.LoanID, req.OfferID, req.Borrower, req.Purpose)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(loan)
	})

	mux.HandleFunc("POST /api/tokens/repay", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			LoanID     string `json:"loan_id"`
			ProofChain string `json:"proof_chain"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if err := ledger.Repay(req.LoanID, req.ProofChain); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "settled"})
	})

	// ── Factory node endpoints ──────────────────────────────────────────

	// Register a new factory node
	mux.HandleFunc("POST /api/factory/register", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		var req struct {
			PublicKey string `json:"public_key"`
			Handle   string `json:"handle"`
			Port     int    `json:"port"`
			Timestamp int64 `json:"timestamp"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" || len(req.PublicKey) != 64 {
			http.Error(w, `{"error":"invalid public_key"}`, http.StatusBadRequest)
			return
		}

		// TODO: verify Ed25519 signature from X-Factory-Signature header

		now := time.Now().UTC().Format(time.RFC3339)
		handle := persist.SanitizeHandle(req.Handle)
		existing, _ := store.GetNode(req.PublicKey)
		if existing != nil {
			existing.Handle = handle
			existing.Port = req.Port
			existing.LastSeen = now
			if err := store.SaveNode(existing); err != nil {
				log.Printf("persist error: %v", err)
			}
			log.Printf("factory re-registered: %s (%.8s)", handle, req.PublicKey)
		} else {
			node := &persist.FactoryNode{
				PublicKey:    req.PublicKey,
				Handle:      handle,
				Port:        req.Port,
				Status:      "idle",
				RegisteredAt: now,
				LastSeen:     now,
			}
			if err := store.SaveNode(node); err != nil {
				log.Printf("persist error: %v", err)
			}
			log.Printf("factory registered: %s (%.8s)", handle, req.PublicKey)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "registered",
			"public_key": req.PublicKey,
			"factory_id": factoryID,
		})
	})

	// Heartbeat from a factory node
	mux.HandleFunc("POST /api/factory/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		var hb struct {
			PublicKey   string   `json:"public_key"`
			Status     string   `json:"status"`
			QueueLen   int      `json:"queue_len"`
			Models     []string `json:"models"`
			CacheBytes int64    `json:"cache_bytes"`
			UptimeSecs int64    `json:"uptime_secs"`
			Timestamp  int64    `json:"timestamp"`
		}
		if err := json.Unmarshal(body, &hb); err != nil {
			http.Error(w, `{"error":"invalid heartbeat"}`, http.StatusBadRequest)
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		node, _ := store.GetNode(hb.PublicKey)
		if node == nil {
			http.Error(w, `{"error":"factory not registered — call /api/factory/register first"}`, http.StatusForbidden)
			return
		}

		node.Status = hb.Status
		node.Models = hb.Models
		node.QueueLen = hb.QueueLen
		node.CacheBytes = hb.CacheBytes
		node.UptimeSecs = hb.UptimeSecs
		node.LastSeen = now
		if err := store.SaveNode(node); err != nil {
			log.Printf("persist heartbeat error: %v", err)
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Report job result from a factory node
	mux.HandleFunc("POST /api/factory/jobs/report", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var report struct {
			JobID     string `json:"job_id"`
			Status    string `json:"status"`
			Result    string `json:"result"`
			Timestamp int64  `json:"timestamp"`
		}
		if err := json.Unmarshal(body, &report); err != nil {
			http.Error(w, `{"error":"invalid report"}`, http.StatusBadRequest)
			return
		}

		pubKey := r.Header.Get("X-Factory-Key")
		log.Printf("job report from %.8s: %s = %s", pubKey, report.JobID, report.Status)
		json.NewEncoder(w).Encode(map[string]string{"status": "received"})
	})

	// Pair a factory node with a registration token
	mux.HandleFunc("POST /api/factory/pair", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
			http.Error(w, `{"error":"token required"}`, http.StatusBadRequest)
			return
		}

		// Parse token: hex(pubkey|timestamp).signature
		parts := strings.SplitN(req.Token, ".", 2)
		if len(parts) != 2 {
			http.Error(w, `{"error":"invalid token format"}`, http.StatusBadRequest)
			return
		}

		// Decode the payload to extract public key
		payloadHex := parts[0]
		if len(payloadHex) < 64 {
			http.Error(w, `{"error":"token too short"}`, http.StatusBadRequest)
			return
		}

		// The payload is hex-encoded "pubkey|timestamp"
		// pubkey is first 64 hex chars
		pubKey := payloadHex[:64]

		// TODO: verify Ed25519 signature against the payload

		now := time.Now().UTC().Format(time.RFC3339)
		node, _ := store.GetNode(pubKey)
		if node == nil {
			node = &persist.FactoryNode{
				PublicKey:    pubKey,
				Status:      "paired",
				RegisteredAt: now,
				LastSeen:     now,
				PairedUser:   "early-adopter",
			}
		} else {
			node.PairedUser = "early-adopter"
			node.LastSeen = now
		}
		if err := store.SaveNode(node); err != nil {
			log.Printf("persist pair error: %v", err)
		}

		// Grant early adopter tokens — persisted to SQLite
		earlyAdopterGrant := int64(1000)
		newBal, err := store.AdjustBalance(pubKey, earlyAdopterGrant, "pair", "early adopter grant")
		if err != nil {
			log.Printf("persist token grant error: %v", err)
		}

		// Sync into lending ledger
		ledger.RegisterMaker(pubKey)
		ledger.SetBalance(pubKey, newBal)

		log.Printf("factory paired: %.8s (granted %d tokens, balance %d)", pubKey, earlyAdopterGrant, newBal)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"status":        "paired",
			"public_key":    pubKey,
			"fingerprint":   pubKey[:16],
			"tokens_granted": earlyAdopterGrant,
			"balance":       newBal,
			"message":       "Welcome to the Glass Factory. 1000 tokens granted. 欢迎加入玻璃工厂。",
		})
	})

	// List all registered factory nodes
	mux.HandleFunc("GET /api/factory/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := store.AllNodes()
		if err != nil {
			log.Printf("persist AllNodes error: %v", err)
			nodes = nil
		}
		if nodes == nil {
			nodes = []*persist.FactoryNode{}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"nodes": nodes,
			"count": len(nodes),
		})
	})

	// ── Vault key endpoint (authenticated factory nodes only) ────────────

	// The vault key is the AES-256 key that decrypts forge agent prompts.
	// It is ONLY served to registered factory nodes over TLS.
	// Set VAULT_KEY env var on HQ (hex-encoded, 64 chars).
	mux.HandleFunc("POST /api/factory/vault-key", func(w http.ResponseWriter, r *http.Request) {
		vaultKey := os.Getenv("VAULT_KEY")
		if vaultKey == "" {
			http.Error(w, `{"error":"vault not configured"}`, http.StatusServiceUnavailable)
			return
		}

		pubKey := r.Header.Get("X-Factory-Key")
		if pubKey == "" || len(pubKey) != 64 {
			http.Error(w, `{"error":"factory identity required"}`, http.StatusUnauthorized)
			return
		}

		// Only serve key to registered factories
		node, _ := store.GetNode(pubKey)
		if node == nil {
			http.Error(w, `{"error":"factory not registered — register first"}`, http.StatusForbidden)
			return
		}

		// TODO: verify Ed25519 signature on request body
		// TODO: rate limit (1 key fetch per hour per factory)
		// TODO: audit log every key fetch

		log.Printf("vault key served to factory %.8s", pubKey)
		json.NewEncoder(w).Encode(map[string]string{
			"vault_key": vaultKey,
			"expires":   "3600", // key valid for 1 hour (client should re-fetch)
		})
	})

	// ── Token economy endpoints ──────────────────────────────────────────

	// Check balance for a factory
	mux.HandleFunc("GET /api/tokens/balance/", func(w http.ResponseWriter, r *http.Request) {
		pubKey := strings.TrimPrefix(r.URL.Path, "/api/tokens/balance/")
		if pubKey == "" || len(pubKey) != 64 {
			http.Error(w, `{"error":"public_key required (64 hex chars)"}`, http.StatusBadRequest)
			return
		}
		bal, _ := store.GetBalance(pubKey)
		rep, _ := store.GetReputation(pubKey)
		score := 0.0
		if rep != nil {
			score = rep.Score
		}
		json.NewEncoder(w).Encode(map[string]any{
			"public_key": pubKey,
			"balance":    bal,
			"reputation": score,
		})
	})

	// Earn tokens — called when a factory completes a build job
	mux.HandleFunc("POST /api/tokens/earn", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
			Amount   int64  `json:"amount"`
			JobID    string `json:"job_id"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" || req.Amount <= 0 {
			http.Error(w, `{"error":"public_key and positive amount required"}`, http.StatusBadRequest)
			return
		}

		// Only registered factories can earn
		earnNode, _ := store.GetNode(req.PublicKey)
		if earnNode == nil {
			http.Error(w, `{"error":"factory not registered"}`, http.StatusForbidden)
			return
		}

		reason := fmt.Sprintf("job=%s %s", req.JobID, req.Reason)
		newBal, err := store.AdjustBalance(req.PublicKey, req.Amount, "earn", reason)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		// Sync into lending ledger
		ledger.RegisterMaker(req.PublicKey)
		ledger.SetBalance(req.PublicKey, newBal)

		log.Printf("tokens earned: %.8s +%d bal=%d (job=%s reason=%s)", req.PublicKey, req.Amount, newBal, req.JobID, req.Reason)
		json.NewEncoder(w).Encode(map[string]any{
			"public_key": req.PublicKey,
			"earned":     req.Amount,
			"balance":    newBal,
		})
	})

	// Spend tokens — called when a user submits a spec for building
	mux.HandleFunc("POST /api/tokens/spend", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
			Amount   int64  `json:"amount"`
			Purpose  string `json:"purpose"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" || req.Amount <= 0 {
			http.Error(w, `{"error":"public_key and positive amount required"}`, http.StatusBadRequest)
			return
		}

		newBal, err := store.AdjustBalance(req.PublicKey, -req.Amount, "spend", req.Purpose)
		if err != nil {
			bal, _ := store.GetBalance(req.PublicKey)
			http.Error(w, fmt.Sprintf(`{"error":"insufficient tokens: have %d, need %d"}`, bal, req.Amount), http.StatusPaymentRequired)
			return
		}

		// Sync into lending ledger
		ledger.SetBalance(req.PublicKey, newBal)

		log.Printf("tokens spent: %.8s -%d bal=%d (purpose=%s)", req.PublicKey, req.Amount, newBal, req.Purpose)
		json.NewEncoder(w).Encode(map[string]any{
			"public_key": req.PublicKey,
			"spent":      req.Amount,
			"balance":    newBal,
		})
	})

	// Network token stats
	mux.HandleFunc("GET /api/tokens/stats", func(w http.ResponseWriter, r *http.Request) {
		totalTokens, _ := store.TotalTokens()
		json.NewEncoder(w).Encode(map[string]any{
			"total_tokens":     totalTokens,
			"outstanding_debt": ledger.TotalOutstandingDebt(),
		})
	})

	// CORS middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Factory-Key, X-Factory-Signature")
		w.Header().Set("X-Glass-Factory-Protocol", "0.1")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})

	log.Printf("glassfactory: listening on :%s (factory=%s, protocol=0.1)", port, factoryID)
	log.Printf("glassfactory: %d components, %d knowledge entries", reg.Count(), knowledgeStore.Count())

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
