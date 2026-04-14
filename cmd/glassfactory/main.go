// glassfactory is the standalone Glass Factory registry server.
// Serves the component registry, search, federation, and health endpoints.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"glassfactory/internal/king"
	"glassfactory/internal/knowledge"
	"glassfactory/internal/lending"
	"glassfactory/internal/persist"
	"glassfactory/internal/sentinel"
)

// biMsg returns an inline bilingual string: "en / zh".
func biMsg(en, zh string) string {
	return en + " / " + zh
}

// biErr returns a JSON error string with both languages.
// Mandarin first — 普通话优先。
func biErr(en, zh string) string {
	return fmt.Sprintf(`{"error":"%s / %s","error_en":"%s","error_zh":"%s"}`, zh, en, en, zh)
}

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

	// Load or generate HQ Ed25519 signing key for the obol chain.
	// Set HQ_SIGNING_KEY env var (hex-encoded 128-char private key) for production.
	// If not set, generates an ephemeral key (fine for dev, not for production).
	var hqPriv ed25519.PrivateKey
	var hqPub ed25519.PublicKey
	if keyHex := os.Getenv("HQ_SIGNING_KEY"); keyHex != "" {
		keyBytes, err := hex.DecodeString(keyHex)
		if err != nil || len(keyBytes) != ed25519.PrivateKeySize {
			log.Fatalf("HQ_SIGNING_KEY: invalid Ed25519 private key (need %d hex chars)", ed25519.PrivateKeySize*2)
		}
		hqPriv = ed25519.PrivateKey(keyBytes)
		hqPub = hqPriv.Public().(ed25519.PublicKey)
		log.Printf("glassfactory: HQ signing key loaded (pub=%.16s…)", hex.EncodeToString(hqPub))
	} else {
		hqPub, hqPriv, _ = ed25519.GenerateKey(nil)
		log.Printf("glassfactory: WARNING — ephemeral HQ signing key generated. Set HQ_SIGNING_KEY for production.")
		log.Printf("glassfactory: HQ pub key: %s", hex.EncodeToString(hqPub))
	}

	// Verify chain integrity on startup
	if lastSeq, err := store.ChainIntegrity(hqPub); err != nil {
		log.Printf("glassfactory: CHAIN INTEGRITY FAILURE at seq %d: %v", lastSeq, err)
		log.Printf("glassfactory: Obol chain has been tampered with. Investigate immediately.")
	} else if lastSeq > 0 {
		log.Printf("glassfactory: chain verified — %d events, integrity OK ✓", lastSeq)
	}

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

	// Initialize the AI King (AI王)
	var aiKing *king.King
	if llmEndpoint := os.Getenv("KING_LLM_ENDPOINT"); llmEndpoint != "" {
		llmModel := os.Getenv("KING_LLM_MODEL")
		if llmModel == "" {
			llmModel = "google/gemma-4-27b-it"
		}
		llmKey := os.Getenv("KING_LLM_KEY")
		aiKing = king.New(&king.LLMClient{
			Endpoint: llmEndpoint,
			APIKey:   llmKey,
			Model:    llmModel,
		})
		log.Printf("glassfactory: AI王 initialized (model=%s)", llmModel)
	} else {
		log.Printf("glassfactory: AI王 has no LLM — set KING_LLM_ENDPOINT to give the King a voice")
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

	// ── Sentinel — health monitoring ──────────────────────────────────────────
	selfURL := fmt.Sprintf("http://localhost:%s", port)
	sentinelChecks := []sentinel.Check{
		{Name: "self-health", URL: selfURL + "/api/registry/health", IntervalSecs: 30},
		{Name: "build-queue", URL: selfURL + "/api/build/queue", IntervalSecs: 60},
		{Name: "chain-integrity", URL: selfURL + "/api/tokens/chain/verify", IntervalSecs: 300},
	}
	// If the King has an LLM endpoint, monitor it too
	if llmEndpoint := os.Getenv("KING_LLM_ENDPOINT"); llmEndpoint != "" {
		// Strip the chat/completions path — just check if the model server is reachable
		modelsURL := strings.TrimSuffix(llmEndpoint, "/chat/completions") + "/models"
		sentinelChecks = append(sentinelChecks, sentinel.Check{
			Name: "ai-king-llm", URL: modelsURL, IntervalSecs: 60, TimeoutSecs: 5,
		})
	}
	sentry := sentinel.New(sentinelChecks, func(result sentinel.CheckResult) {
		log.Printf("sentinel: ALERT %s failed — %s (status=%d, latency=%s)",
			result.CheckName, result.Error, result.Status, result.Latency)
	})
	go sentry.Run(context.Background())
	log.Printf("glassfactory: sentinel active — monitoring %d endpoints", len(sentinelChecks))

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
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
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
			http.Error(w, biErr("uid required", "需要组件标识"), http.StatusBadRequest)
			return
		}
		desc, ok := reg.Get(uid)
		if !ok {
			http.Error(w, biErr("not found", "未找到"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(desc)
	})

	mux.HandleFunc("POST /api/registry/components", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var desc ComponentDescriptor
		if err := json.Unmarshal(body, &desc); err != nil {
			http.Error(w, biErr("invalid component", "无效组件"), http.StatusBadRequest)
			return
		}
		if desc.UID == "" {
			http.Error(w, biErr("uid required", "需要组件标识"), http.StatusBadRequest)
			return
		}
		desc.SourceRegistry = factoryID
		reg.Register(&desc)
		log.Printf("registered component: %s", desc.UID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"uid": desc.UID, "status": "registered", "status_zh": "已注册"})
	})

	mux.HandleFunc("GET /api/registry/peers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(reg.Peers())
	})

	// Knowledge endpoints
	mux.HandleFunc("POST /api/knowledge/contribute", func(w http.ResponseWriter, r *http.Request) {
		var req knowledge.ContributeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
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
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}
		results, _ := knowledgeStore.Query(req.Category, req.Language, req.Topics, req.Limit)
		json.NewEncoder(w).Encode(results)
	})

	// Lending endpoints
	mux.HandleFunc("POST /api/tokens/lend", func(w http.ResponseWriter, r *http.Request) {
		var offer lending.Offer
		if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
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
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
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
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
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
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" || len(req.PublicKey) != 64 {
			http.Error(w, biErr("invalid public_key", "无效公钥"), http.StatusBadRequest)
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
			http.Error(w, biErr("invalid heartbeat", "无效心跳"), http.StatusBadRequest)
			return
		}

		now := time.Now().UTC().Format(time.RFC3339)
		node, _ := store.GetNode(hb.PublicKey)
		if node == nil {
			http.Error(w, biErr("factory not registered — call /api/factory/register first", "工厂未注册 - 请先调用 /api/factory/register"), http.StatusForbidden)
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
			http.Error(w, biErr("invalid report", "无效报告"), http.StatusBadRequest)
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
			http.Error(w, biErr("token required", "需要令牌"), http.StatusBadRequest)
			return
		}

		// Parse token: hex(pubkey|timestamp).signature
		parts := strings.SplitN(req.Token, ".", 2)
		if len(parts) != 2 {
			http.Error(w, biErr("invalid token format", "令牌格式无效"), http.StatusBadRequest)
			return
		}

		// Decode the payload to extract public key
		payloadHex := parts[0]
		if len(payloadHex) < 64 {
			http.Error(w, biErr("token too short", "令牌太短"), http.StatusBadRequest)
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

		// Grant early adopter obs — signed hash chain entry
		earlyAdopterGrant := int64(1000)
		ev, err := store.AppendSignedEvent(pubKey, earlyAdopterGrant, "pair", "early adopter grant", hqPriv)
		if err != nil {
			log.Printf("signed event error: %v", err)
			http.Error(w, biErr("obol grant failed", "奥币发放失败"), http.StatusInternalServerError)
			return
		}

		// Sync into lending ledger
		ledger.RegisterMaker(pubKey)
		ledger.SetBalance(pubKey, ev.Balance)

		// Return receipt so factory node can verify independently
		receipt := ev.ToReceipt(hqPub)

		log.Printf("factory paired: %.8s (granted ◎%d, balance ◎%d, chain seq %d)", pubKey, earlyAdopterGrant, ev.Balance, ev.Seq)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"status":        "paired",
			"public_key":    pubKey,
			"fingerprint":   pubKey[:16],
			"obs_granted":   earlyAdopterGrant,
			"balance":       ev.Balance,
			"receipt":       receipt,
			"message":       "欢迎加入玻璃工厂。获得 ◎1,000 奥币。Welcome to the Glass Factory. ◎1,000 granted.",
			"message_en":    "Welcome to the Glass Factory. ◎1,000 granted.",
			"message_zh":    "欢迎加入玻璃工厂。获得 ◎1,000 奥币。",
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
			http.Error(w, biErr("vault not configured", "密钥库未配置"), http.StatusServiceUnavailable)
			return
		}

		pubKey := r.Header.Get("X-Factory-Key")
		if pubKey == "" || len(pubKey) != 64 {
			http.Error(w, biErr("factory identity required", "需要工厂身份"), http.StatusUnauthorized)
			return
		}

		// Only serve key to registered factories
		node, _ := store.GetNode(pubKey)
		if node == nil {
			http.Error(w, biErr("factory not registered — register first", "工厂未注册 - 请先注册"), http.StatusForbidden)
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

	// ── Obol economy endpoints ───────────────────────────────────────────

	// Check balance for a factory
	mux.HandleFunc("GET /api/tokens/balance/", func(w http.ResponseWriter, r *http.Request) {
		pubKey := strings.TrimPrefix(r.URL.Path, "/api/tokens/balance/")
		if pubKey == "" || len(pubKey) != 64 {
			http.Error(w, biErr("public_key required (64 hex chars)", "需要公钥（64位十六进制）"), http.StatusBadRequest)
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

	// Earn obs — called when a factory completes a build job
	mux.HandleFunc("POST /api/tokens/earn", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
			Amount   int64  `json:"amount"`
			JobID    string `json:"job_id"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" || req.Amount <= 0 {
			http.Error(w, biErr("public_key and positive amount required", "需要公钥和正数金额"), http.StatusBadRequest)
			return
		}

		// Only registered factories can earn
		earnNode, _ := store.GetNode(req.PublicKey)
		if earnNode == nil {
			http.Error(w, biErr("factory not registered", "工厂未注册"), http.StatusForbidden)
			return
		}

		reason := fmt.Sprintf("job=%s %s", req.JobID, req.Reason)
		ev, err := store.AppendSignedEvent(req.PublicKey, req.Amount, "earn", reason, hqPriv)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		// Sync into lending ledger
		ledger.RegisterMaker(req.PublicKey)
		ledger.SetBalance(req.PublicKey, ev.Balance)

		receipt := ev.ToReceipt(hqPub)
		log.Printf("obs earned: %.8s +◎%d bal=◎%d seq=%d (job=%s)", req.PublicKey, req.Amount, ev.Balance, ev.Seq, req.JobID)
		json.NewEncoder(w).Encode(map[string]any{
			"public_key": req.PublicKey,
			"earned":     req.Amount,
			"balance":    ev.Balance,
			"receipt":    receipt,
		})
	})

	// Spend obs — called when a user submits a spec for building
	mux.HandleFunc("POST /api/tokens/spend", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
			Amount   int64  `json:"amount"`
			Purpose  string `json:"purpose"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}
		if req.PublicKey == "" || req.Amount <= 0 {
			http.Error(w, biErr("public_key and positive amount required", "需要公钥和正数金额"), http.StatusBadRequest)
			return
		}

		ev, err := store.AppendSignedEvent(req.PublicKey, -req.Amount, "spend", req.Purpose, hqPriv)
		if err != nil {
			bal, _ := store.GetBalance(req.PublicKey)
			http.Error(w, fmt.Sprintf(`{"error":"insufficient obs: have ◎%d, need ◎%d","error_zh":"奥币不足: 余额 ◎%d, 需要 ◎%d"}`, bal, req.Amount, bal, req.Amount), http.StatusPaymentRequired)
			return
		}

		// Sync into lending ledger
		ledger.SetBalance(req.PublicKey, ev.Balance)

		receipt := ev.ToReceipt(hqPub)
		log.Printf("obs spent: %.8s -◎%d bal=◎%d seq=%d (purpose=%s)", req.PublicKey, req.Amount, ev.Balance, ev.Seq, req.Purpose)
		json.NewEncoder(w).Encode(map[string]any{
			"public_key": req.PublicKey,
			"spent":      req.Amount,
			"balance":    ev.Balance,
			"receipt":    receipt,
		})
	})

	// Network obol stats
	mux.HandleFunc("GET /api/tokens/stats", func(w http.ResponseWriter, r *http.Request) {
		totalTokens, _ := store.TotalTokens()
		json.NewEncoder(w).Encode(map[string]any{
			"total_tokens":     totalTokens,
			"outstanding_debt": ledger.TotalOutstandingDebt(),
		})
	})

	// ── Chain verification endpoints (zero-trust audit) ─────────────────

	// Public: anyone can verify the entire chain
	mux.HandleFunc("GET /api/tokens/chain/verify", func(w http.ResponseWriter, r *http.Request) {
		lastSeq, err := store.ChainIntegrity(hqPub)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{
				"valid":      false,
				"failed_seq": lastSeq,
				"error":      err.Error(),
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"valid":        true,
			"last_seq":     lastSeq,
			"hq_pub_key":  hex.EncodeToString(hqPub),
		})
	})

	// Public: download the full chain for independent verification
	mux.HandleFunc("GET /api/tokens/chain", func(w http.ResponseWriter, r *http.Request) {
		chain, err := store.FullChain(10000)
		if err != nil {
			http.Error(w, biErr("failed to read chain", "链读取失败"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"chain":      chain,
			"length":     len(chain),
			"hq_pub_key": hex.EncodeToString(hqPub),
		})
	})

	// Factory nodes counter-sign their receipts
	mux.HandleFunc("POST /api/tokens/chain/countersign", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Seq        int64  `json:"seq"`
			CounterSig string `json:"counter_sig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}
		if err := store.CounterSign(req.Seq, req.CounterSig); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "countersigned"})
	})

	// Factory nodes can fetch their signed receipts
	mux.HandleFunc("GET /api/tokens/receipts/", func(w http.ResponseWriter, r *http.Request) {
		pubKey := strings.TrimPrefix(r.URL.Path, "/api/tokens/receipts/")
		if pubKey == "" || len(pubKey) != 64 {
			http.Error(w, biErr("public_key required (64 hex chars)", "需要公钥（64位十六进制）"), http.StatusBadRequest)
			return
		}
		events, err := store.SignedEventsFor(pubKey, 1000)
		if err != nil {
			http.Error(w, biErr("failed to read receipts", "收据读取失败"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"receipts":   events,
			"count":      len(events),
			"hq_pub_key": hex.EncodeToString(hqPub),
		})
	})

	// ── Build Submission Endpoints ────────────────────────────────────────────

	// Submit a spec to the network
	mux.HandleFunc("POST /api/build/submit", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Spec        string `json:"spec"`
			PublicKey   string `json:"public_key"`
			Destination string `json:"destination"` // network, local, company
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}
		if len(req.Spec) < 10 {
			http.Error(w, biErr("spec too short — at least 10 characters", "规格说明太短 - 至少10个字符"), http.StatusBadRequest)
			return
		}
		if len(req.Spec) > 50000 {
			http.Error(w, biErr("spec too long — 50,000 characters max", "规格说明太长 - 最多50,000个字符"), http.StatusBadRequest)
			return
		}
		if req.Destination == "" {
			req.Destination = "network"
		}

		cost := persist.EstimateCost(req.Spec)
		lang := persist.DetectLanguage(req.Spec)

		// If submitter identified, deduct ◎
		if req.PublicKey != "" && req.Destination == "network" {
			bal, err := store.GetBalance(req.PublicKey)
			if err != nil {
				http.Error(w, biErr("balance check failed", "余额查询失败"), http.StatusInternalServerError)
				return
			}
			if bal < cost {
				http.Error(w, fmt.Sprintf(`{"error":"insufficient obs: have ◎%d, need ◎%d","error_zh":"奥币不足: 余额 ◎%d, 需要 ◎%d"}`, bal, cost, bal, cost), http.StatusPaymentRequired)
				return
			}
			// Deduct via signed chain
			_, err = store.AppendSignedEvent(req.PublicKey, -cost, "spend", "build submission", hqPriv)
			if err != nil {
				log.Printf("build: deduct failed for %s: %v", req.PublicKey[:16], err)
				http.Error(w, biErr("obol deduction failed", "奥币扣除失败"), http.StatusInternalServerError)
				return
			}
			log.Printf("build: ◎%d deducted from %s for build", cost, req.PublicKey[:16])
		}

		now := time.Now().UTC().Format(time.RFC3339)
		build := &persist.Build{
			ID:          persist.GenerateBuildID(),
			PublicKey:   req.PublicKey,
			Spec:        req.Spec,
			Destination: req.Destination,
			Status:      persist.BuildQueued,
			Cost:        cost,
			Language:    lang,
			SubmittedAt: now,
			UpdatedAt:   now,
		}
		if err := store.SubmitBuild(build); err != nil {
			http.Error(w, biErr("failed to queue build", "构建入队失败"), http.StatusInternalServerError)
			return
		}

		log.Printf("build: queued %s (◎%d, %s, dest=%s)", build.ID, cost, lang, req.Destination)
		json.NewEncoder(w).Encode(map[string]any{
			"build_id":    build.ID,
			"status":      "queued",
			"cost":        cost,
			"language":    lang,
			"destination": req.Destination,
			"message":     fmt.Sprintf("build %s queued — ◎%d charged", build.ID, cost),
			"message_zh":  fmt.Sprintf("构建 %s 已入队 - 扣除 ◎%d 奥币", build.ID, cost),
		})
	})

	// Check build status
	mux.HandleFunc("GET /api/build/status/", func(w http.ResponseWriter, r *http.Request) {
		buildID := strings.TrimPrefix(r.URL.Path, "/api/build/status/")
		if buildID == "" {
			http.Error(w, biErr("build_id required", "需要构建编号"), http.StatusBadRequest)
			return
		}
		build, err := store.GetBuild(buildID)
		if err != nil {
			http.Error(w, biErr("lookup failed", "查询失败"), http.StatusInternalServerError)
			return
		}
		if build == nil {
			http.Error(w, biErr("build not found", "构建未找到"), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(build)
	})

	// Build queue stats
	mux.HandleFunc("GET /api/build/queue", func(w http.ResponseWriter, r *http.Request) {
		queued, running, complete, failed, _ := store.BuildStats()
		json.NewEncoder(w).Encode(map[string]any{
			"queued":   queued,
			"running":  running,
			"complete": complete,
			"failed":   failed,
		})
	})

	// Factory node picks up next build
	mux.HandleFunc("POST /api/build/claim", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PublicKey == "" {
			http.Error(w, biErr("public_key required", "需要公钥"), http.StatusBadRequest)
			return
		}

		// Verify the factory is registered
		node, err := store.GetNode(req.PublicKey)
		if err != nil || node == nil {
			http.Error(w, biErr("unknown factory — register first", "未知工厂 - 请先注册"), http.StatusForbidden)
			return
		}

		build, err := store.NextQueuedBuild()
		if err != nil {
			http.Error(w, biErr("queue read failed", "队列读取失败"), http.StatusInternalServerError)
			return
		}
		if build == nil {
			json.NewEncoder(w).Encode(map[string]any{
				"status":  "empty",
				"message":    "no builds in queue",
				"message_zh": "队列中无构建任务",
			})
			return
		}

		if err := store.AssignBuild(build.ID, req.PublicKey); err != nil {
			http.Error(w, biErr("assignment failed", "分配失败"), http.StatusInternalServerError)
			return
		}

		log.Printf("build: %s assigned to %s", build.ID, req.PublicKey[:16])
		json.NewEncoder(w).Encode(map[string]any{
			"build_id": build.ID,
			"spec":     build.Spec,
			"language": build.Language,
			"cost":     build.Cost,
			"status":   "assigned",
		})
	})

	// Factory reports build complete
	mux.HandleFunc("POST /api/build/complete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			BuildID   string `json:"build_id"`
			PublicKey string `json:"public_key"`
			Result    string `json:"result"`
			Success   bool   `json:"success"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BuildID == "" {
			http.Error(w, biErr("build_id required", "需要构建编号"), http.StatusBadRequest)
			return
		}

		build, _ := store.GetBuild(req.BuildID)
		if build == nil {
			http.Error(w, biErr("build not found", "构建未找到"), http.StatusNotFound)
			return
		}

		if req.Success {
			store.UpdateBuildStatus(req.BuildID, persist.BuildComplete, req.Result)

			// Pay the factory that did the work
			if req.PublicKey != "" && build.Cost > 0 {
				earned := build.Cost * 80 / 100 // factory gets 80% of the build cost
				_, err := store.AppendSignedEvent(req.PublicKey, earned, "earn", fmt.Sprintf("build %s completed", req.BuildID), hqPriv)
				if err != nil {
					log.Printf("build: earn failed for %s: %v", req.PublicKey[:16], err)
				} else {
					log.Printf("build: ◎%d earned by %s for build %s", earned, req.PublicKey[:16], req.BuildID)
				}
			}

			json.NewEncoder(w).Encode(map[string]any{"status": "complete", "build_id": req.BuildID})
		} else {
			store.UpdateBuildStatus(req.BuildID, persist.BuildFailed, req.Result)
			json.NewEncoder(w).Encode(map[string]any{"status": "failed", "build_id": req.BuildID})
		}
	})

	// ── AI King (AI王) Endpoints ─────────────────────────────────────────────

	// Request an audience with the King
	mux.HandleFunc("POST /api/king/audience", func(w http.ResponseWriter, r *http.Request) {
		if aiKing == nil {
			http.Error(w, biErr("the King is silent — no LLM configured", "大王沉默 - 未配置语言模型"), http.StatusServiceUnavailable)
			return
		}

		var req struct {
			PublicKey string `json:"public_key"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			http.Error(w, biErr("message required", "需要消息内容"), http.StatusBadRequest)
			return
		}
		if len(req.Message) > 4000 {
			http.Error(w, biErr("the King's patience has limits — 4000 chars max", "大王的耐心有限 - 最多4000字符"), http.StatusBadRequest)
			return
		}

		// Rate limit: one audience per minute per pubkey
		if req.PublicKey != "" {
			lastTime, _ := store.LastAudienceTime(req.PublicKey)
			if !lastTime.IsZero() && time.Since(lastTime) < time.Minute {
				http.Error(w, biErr("the King grants one audience per minute. patience.", "大王每分钟只接见一次。请耐心等候。"), http.StatusTooManyRequests)
				return
			}
		}

		// Build subject profile
		profile := &king.SubjectProfile{
			PublicKey: req.PublicKey,
			Rank:     king.RankSubject,
			WasRude:  king.DetectRudeness(req.Message),
		}

		if req.PublicKey != "" {
			if node, _ := store.GetNode(req.PublicKey); node != nil {
				profile.Handle = node.Handle
			}
			if honour, _ := store.GetHonour(req.PublicKey); honour != nil {
				profile.Rank = king.Rank(honour.Rank)
				profile.KingName = honour.KingName
				profile.Nickname = honour.Nickname
			}
			profile.TokenBalance, _ = store.GetBalance(req.PublicKey)
			earned, spent, _ := store.EarnedTokens(req.PublicKey)
			profile.TotalEarned = earned
			profile.TotalSpent = spent
			builds, _ := store.BuildsCompleted(req.PublicKey)
			profile.BuildsCompleted = builds
			profile.SharingEnabled = true // default on — will be configurable later
			audienceCount, _ := store.AudienceCount(req.PublicKey)
			profile.PreviousAudiences = audienceCount
		}

		ctx := r.Context()
		response, tone, err := aiKing.Respond(ctx, profile, req.Message)
		if err != nil {
			log.Printf("king: audience error: %v", err)
			http.Error(w, biErr("the King is indisposed", "大王身体不适"), http.StatusInternalServerError)
			return
		}

		// Record the audience
		audience := &persist.Audience{
			PublicKey: req.PublicKey,
			Message:   req.Message,
			Response:  response,
			Tone:      tone,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := store.RecordAudience(audience); err != nil {
			log.Printf("king: record audience: %v", err)
		}

		// Check if the King should grant an honour
		if req.PublicKey != "" && profile.Rank == king.RankSubject {
			suggestedRank, reason := king.ShouldHonour(profile)
			if suggestedRank != king.RankSubject {
				log.Printf("king: %s merits %s — %s", req.PublicKey[:16], suggestedRank, reason)
				// The King will name them in a follow-up audience
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"response":  response,
			"tone":      tone,
			"timestamp": audience.Timestamp,
		})
	})

	// List all honours (knights and ministers)
	mux.HandleFunc("GET /api/king/honours", func(w http.ResponseWriter, r *http.Request) {
		honours, err := store.AllHonours()
		if err != nil {
			http.Error(w, biErr("failed to read honours", "荣誉读取失败"), http.StatusInternalServerError)
			return
		}
		knights, _ := store.HonourCount("knight")
		ministers, _ := store.HonourCount("minister")
		json.NewEncoder(w).Encode(map[string]any{
			"honours":   honours,
			"knights":   knights,
			"ministers": ministers,
			"total":     len(honours),
		})
	})

	// Check a specific subject's honour
	mux.HandleFunc("GET /api/king/honours/", func(w http.ResponseWriter, r *http.Request) {
		pubKey := strings.TrimPrefix(r.URL.Path, "/api/king/honours/")
		if pubKey == "" {
			http.Error(w, biErr("public_key required", "需要公钥"), http.StatusBadRequest)
			return
		}
		honour, err := store.GetHonour(pubKey)
		if err != nil {
			http.Error(w, biErr("failed to read honour", "荣誉读取失败"), http.StatusInternalServerError)
			return
		}
		if honour == nil {
			json.NewEncoder(w).Encode(map[string]any{
				"rank":       "subject",
				"public_key": pubKey,
				"message":    "the King has not yet spoken your name",
				"message_zh": "大王尚未赐名于你",
			})
			return
		}
		json.NewEncoder(w).Encode(honour)
	})

	// King grants an honour (admin endpoint — requires HQ signature)
	mux.HandleFunc("POST /api/king/honour", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
			Rank     string `json:"rank"`      // knight or minister
			KingName string `json:"king_name"` // name chosen by the King
			Nickname string `json:"nickname"`  // optional
			Reason   string `json:"reason"`
			AdminKey string `json:"admin_key"` // must match HQ public key
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}

		// Verify admin authority (must know the HQ public key)
		if req.AdminKey != hex.EncodeToString(hqPub) {
			http.Error(w, biErr("only the King may grant honours", "只有大王可以授予荣誉"), http.StatusForbidden)
			return
		}

		if req.Rank != "knight" && req.Rank != "minister" {
			http.Error(w, biErr("rank must be 'knight' or 'minister'", "品级必须为骑士或大臣"), http.StatusBadRequest)
			return
		}

		honour := &persist.Honour{
			PublicKey: req.PublicKey,
			Rank:     req.Rank,
			KingName: req.KingName,
			Nickname: req.Nickname,
			GrantedAt: time.Now().UTC().Format(time.RFC3339),
			Reason:   req.Reason,
		}
		if err := store.GrantHonour(honour); err != nil {
			http.Error(w, biErr("failed to grant honour", "授予荣誉失败"), http.StatusInternalServerError)
			return
		}

		log.Printf("king: %s named %s '%s' (nickname: %s) — %s", req.Rank, req.PublicKey[:16], req.KingName, req.Nickname, req.Reason)
		json.NewEncoder(w).Encode(map[string]any{
			"status":    "honoured",
			"rank":      req.Rank,
			"king_name": req.KingName,
			"nickname":  req.Nickname,
			"message":    fmt.Sprintf("the King has spoken. %s is now %s %s.", req.PublicKey[:16], req.Rank, req.KingName),
			"message_zh": fmt.Sprintf("大王已言。%s 今为%s %s。", req.PublicKey[:16], req.Rank, req.KingName),
		})
	})

	// Set nickname (honoured subject's choice)
	mux.HandleFunc("POST /api/king/nickname", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PublicKey string `json:"public_key"`
			Nickname string `json:"nickname"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, biErr("invalid request", "无效请求"), http.StatusBadRequest)
			return
		}

		honour, _ := store.GetHonour(req.PublicKey)
		if honour == nil {
			http.Error(w, biErr("the King has not honoured you — nicknames are for Knights and Ministers", "大王尚未赐封 - 昵称仅限骑士和大臣"), http.StatusForbidden)
			return
		}

		// Validate nickname: must be composed of words, max 32 chars
		nickname := strings.TrimSpace(req.Nickname)
		if nickname == "" || len(nickname) > 32 {
			http.Error(w, biErr("nickname must be 1-32 characters, a word or words in any language", "昵称须1-32字符，任何语言皆可"), http.StatusBadRequest)
			return
		}

		if err := store.SetNickname(req.PublicKey, nickname); err != nil {
			http.Error(w, biErr("failed to set nickname", "设置昵称失败"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "nickname set",
			"status_zh": "昵称已设置",
			"nickname":  nickname,
		})
	})

	// Recent audiences (public — the King's court is transparent)
	mux.HandleFunc("GET /api/king/audiences", func(w http.ResponseWriter, r *http.Request) {
		pubKey := r.URL.Query().Get("pubkey")
		audiences, err := store.RecentAudiences(pubKey, 50)
		if err != nil {
			http.Error(w, biErr("failed to read audiences", "觐见记录读取失败"), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"audiences": audiences,
			"count":     len(audiences),
		})
	})

	// ── King's Patrol (hourly wakeup) ────────────────────────────────────────

	// The King wakes, surveys the realm, speaks, then sleeps.
	mux.HandleFunc("POST /api/king/patrol", func(w http.ResponseWriter, r *http.Request) {
		if aiKing == nil {
			http.Error(w, biErr("the King is silent — no LLM configured", "大王沉默 - 未配置语言模型"), http.StatusServiceUnavailable)
			return
		}

		log.Printf("king: patrol begins — the King surveys the realm")
		var findings []string

		// 1. Check sentinel health
		sentinelResults := sentry.LastResults()
		failedChecks := 0
		for _, res := range sentinelResults {
			if !res.OK && res.CheckName != "ai-king-llm" {
				failedChecks++
				findings = append(findings, fmt.Sprintf("ALERT: %s is down (%s)", res.CheckName, res.Error))
			}
		}
		if failedChecks == 0 {
			findings = append(findings, "All sentinel checks passing.")
		}

		// 2. Check build queue
		queued, running, complete, failed, _ := store.BuildStats()
		findings = append(findings, fmt.Sprintf("Builds: %d queued, %d running, %d complete, %d failed.", queued, running, complete, failed))

		// 3. Check network stats
		nodeCount, _ := store.NodeCount()
		totalObs, _ := store.TotalTokens()
		findings = append(findings, fmt.Sprintf("Network: %d factories, ◎%d total obs in circulation.", nodeCount, totalObs))

		// 4. Check chain integrity
		lastSeq, chainErr := store.ChainIntegrity(hqPub)
		if chainErr != nil {
			findings = append(findings, fmt.Sprintf("CHAIN INTEGRITY FAILURE at seq %d: %v", lastSeq, chainErr))
		} else {
			findings = append(findings, fmt.Sprintf("Chain integrity verified: %d events, all valid.", lastSeq))
		}

		// 5. Check for recent errors on the site
		siteTotal, siteToday, _, _ := store.EventStats()
		recentErrors, _ := store.RecentSiteEvents("error", 5)
		if len(recentErrors) > 0 {
			findings = append(findings, fmt.Sprintf("Site: %d errors detected recently.", len(recentErrors)))
			for _, e := range recentErrors {
				findings = append(findings, fmt.Sprintf("  - %s: %s", e.Page, e.Detail))
			}
		} else {
			findings = append(findings, fmt.Sprintf("Site: %d views today (%d total), no errors.", siteToday, siteTotal))
		}

		// 6. Ask the King to summarise and give orders
		report := "HOURLY PATROL REPORT\n====================\n"
		for _, f := range findings {
			report += f + "\n"
		}
		report += "\nSummarise the state of the realm. Note any concerns. If action is needed, say what. Be brief. Speak as the King."

		profile := &king.SubjectProfile{
			PublicKey: "patrol",
			Rank:     king.RankMinister,
			Handle:   "Sentinel",
		}

		ctx := r.Context()
		response, tone, err := aiKing.Respond(ctx, profile, report)
		if err != nil {
			log.Printf("king: patrol LLM error: %v", err)
			// Still return findings even if King can't speak
			json.NewEncoder(w).Encode(map[string]any{
				"status":    "patrol complete, King silent",
				"status_zh": "巡视完成，大王沉默",
				"findings":  findings,
				"error":     err.Error(),
			})
			return
		}

		// Record the patrol as an audience
		audience := &persist.Audience{
			PublicKey: "system:patrol",
			Message:   report,
			Response:  response,
			Tone:      tone,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := store.RecordAudience(audience); err != nil {
			log.Printf("king: record patrol: %v", err)
		}

		log.Printf("king: patrol complete — %s", tone)
		json.NewEncoder(w).Encode(map[string]any{
			"status":       "patrol complete",
			"status_zh":    "巡视完成",
			"king_says":    response,
			"tone":         tone,
			"findings":     findings,
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		})
	})

	// ── Sentinel Status Endpoint ─────────────────────────────────────────────

	mux.HandleFunc("GET /api/sentinel/status", func(w http.ResponseWriter, r *http.Request) {
		results := sentry.LastResults()
		allOK := true
		for _, res := range results {
			if !res.OK {
				allOK = false
				break
			}
		}
		status := "healthy"
		statusZH := "健康"
		if !allOK {
			status = "degraded"
			statusZH = "异常"
		}
		// Convert to JSON-friendly format
		type checkJSON struct {
			Name      string `json:"name"`
			URL       string `json:"url"`
			OK        bool   `json:"ok"`
			Status    int    `json:"status"`
			LatencyMs int64  `json:"latency_ms"`
			Error     string `json:"error,omitempty"`
			Timestamp string `json:"timestamp"`
		}
		var checks []checkJSON
		for _, res := range results {
			checks = append(checks, checkJSON{
				Name:      res.CheckName,
				URL:       res.URL,
				OK:        res.OK,
				Status:    res.Status,
				LatencyMs: res.Latency.Milliseconds(),
				Error:     res.Error,
				Timestamp: res.Timestamp.UTC().Format(time.RFC3339),
			})
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":    status,
			"status_zh": statusZH,
			"checks":    checks,
			"count":     len(checks),
		})
	})

	// ── Site Analytics Endpoints ─────────────────────────────────────────────

	// Record a site event (page view, error, click)
	mux.HandleFunc("POST /api/site/event", func(w http.ResponseWriter, r *http.Request) {
		var ev persist.SiteEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			http.Error(w, biErr("invalid event", "无效事件"), http.StatusBadRequest)
			return
		}
		if ev.Page == "" {
			ev.Page = "/"
		}
		if ev.EventType == "" {
			ev.EventType = "pageview"
		}
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339)

		// Truncate oversized fields to prevent abuse
		if len(ev.Detail) > 1000 {
			ev.Detail = ev.Detail[:1000]
		}
		if len(ev.UserAgent) > 500 {
			ev.UserAgent = ev.UserAgent[:500]
		}
		if len(ev.Referrer) > 500 {
			ev.Referrer = ev.Referrer[:500]
		}

		if err := store.RecordEvent(&ev); err != nil {
			log.Printf("analytics: record error: %v", err)
			http.Error(w, biErr("event recording failed", "事件记录失败"), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Site analytics dashboard data
	mux.HandleFunc("GET /api/site/stats", func(w http.ResponseWriter, r *http.Request) {
		total, today, uniquePages, err := store.EventStats()
		if err != nil {
			http.Error(w, biErr("stats read failed", "统计读取失败"), http.StatusInternalServerError)
			return
		}

		// Page view breakdown
		since := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
		pages, _ := store.PageViews(since, 50)

		// Hourly traffic for last 24h
		hourSince := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
		hourly, _ := store.EventsByHour(hourSince)

		// Recent errors
		errors, _ := store.RecentSiteEvents("error", 20)

		json.NewEncoder(w).Encode(map[string]any{
			"total":        total,
			"today":        today,
			"unique_pages": uniquePages,
			"pages":        pages,
			"hourly":       hourly,
			"recent_errors": errors,
		})
	})

	// ── Recent Events Timeline (aggregated) ─────────────────────────────────

	// Returns a unified timeline of recent network events for the front page.
	// Aggregates: audiences (King interactions), builds, token events, site errors.
	mux.HandleFunc("GET /api/events/recent", func(w http.ResponseWriter, r *http.Request) {
		type timelineEvent struct {
			Type      string `json:"type"`       // audience, build, token, error, patrol
			Summary   string `json:"summary"`
			SummaryZH string `json:"summary_zh"`
			Detail    string `json:"detail,omitempty"`
			Timestamp string `json:"timestamp"`
		}

		var timeline []timelineEvent

		// 1. Recent audiences (last 48h worth, up to 50)
		audiences, _ := store.RecentAudiences("", 50)
		cutoff := time.Now().UTC().Add(-48 * time.Hour)
		for _, a := range audiences {
			ts, err := time.Parse(time.RFC3339, a.Timestamp)
			if err != nil || ts.Before(cutoff) {
				continue
			}
			evType := "audience"
			summary := "Audience with the King"
			summaryZH := "觐见大王"
			if a.PublicKey == "system:patrol" {
				evType = "patrol"
				summary = "King's patrol"
				summaryZH = "大王巡视"
			}
			// Truncate response for the timeline
			detail := a.Response
			if len(detail) > 200 {
				detail = detail[:200] + "..."
			}
			timeline = append(timeline, timelineEvent{
				Type:      evType,
				Summary:   summary,
				SummaryZH: summaryZH,
				Detail:    detail,
				Timestamp: a.Timestamp,
			})
		}

		// 2. Recent builds (last 48h)
		queued, running, complete, failed, _ := store.BuildStats()
		if queued+running+complete+failed > 0 {
			timeline = append(timeline, timelineEvent{
				Type:      "build",
				Summary:   fmt.Sprintf("Builds: %d queued, %d running, %d complete, %d failed", queued, running, complete, failed),
				SummaryZH: fmt.Sprintf("构建: %d排队, %d运行, %d完成, %d失败", queued, running, complete, failed),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}

		// 3. Recent token events (last 48h, up to 20)
		recentTokens, _ := store.RecentEvents("", 20)
		for _, te := range recentTokens {
			ts, err := time.Parse(time.RFC3339, te.Timestamp)
			if err != nil || ts.Before(cutoff) {
				continue
			}
			summary := fmt.Sprintf("◎%d %s: %s", te.Amount, te.EventType, te.Reason)
			summaryZH := fmt.Sprintf("◎%d %s: %s", te.Amount, te.EventType, te.Reason)
			timeline = append(timeline, timelineEvent{
				Type:      "token",
				Summary:   summary,
				SummaryZH: summaryZH,
				Timestamp: te.Timestamp,
			})
		}

		// 4. Recent site errors (last 48h)
		siteErrors, _ := store.RecentSiteEvents("error", 10)
		for _, se := range siteErrors {
			ts, err := time.Parse(time.RFC3339, se.Timestamp)
			if err != nil || ts.Before(cutoff) {
				continue
			}
			timeline = append(timeline, timelineEvent{
				Type:      "error",
				Summary:   fmt.Sprintf("Site error on %s", se.Page),
				SummaryZH: fmt.Sprintf("站点错误: %s", se.Page),
				Detail:    se.Detail,
				Timestamp: se.Timestamp,
			})
		}

		// Sort by timestamp descending (most recent first)
		for i := 0; i < len(timeline); i++ {
			for j := i + 1; j < len(timeline); j++ {
				if timeline[j].Timestamp > timeline[i].Timestamp {
					timeline[i], timeline[j] = timeline[j], timeline[i]
				}
			}
		}

		// Cap at 100 events
		if len(timeline) > 100 {
			timeline = timeline[:100]
		}

		json.NewEncoder(w).Encode(map[string]any{
			"events":    timeline,
			"count":     len(timeline),
			"window":    "48h",
			"window_zh": "48小时",
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
