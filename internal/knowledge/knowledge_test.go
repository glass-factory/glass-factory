package knowledge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func testEntry(pub ed25519.PublicKey, priv ed25519.PrivateKey) *Entry {
	e := &Entry{
		ID:             "k-001",
		Category:       "pattern",
		Topic:          "Go HTTP middleware ordering",
		Content:        "Auth middleware must run before rate limiting to prevent unauthenticated requests consuming rate limit budget.",
		Language:       "go",
		Confidence:     0.85,
		SourceJob:      "forge_123",
		SourceFactory:  "https://home.darkfactory.dev",
		Contributors:   1,
		ProofChainHash: HashProofChain(`[{"stage":"code","result_hash":"abc123"}]`),
	}
	SignEntry(e, priv, pub)
	return e
}

func TestSignAndVerify(t *testing.T) {
	pub, priv := testKey(t)
	e := testEntry(pub, priv)

	valid, err := VerifyEntry(e)
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("entry signature should be valid")
	}

	// Tamper
	e.Content = "tampered"
	valid, _ = VerifyEntry(e)
	if valid {
		t.Fatal("tampered entry should fail verification")
	}
}

func TestSaveAndQuery(t *testing.T) {
	pub, priv := testKey(t)
	store := NewMemStore()

	e := testEntry(pub, priv)
	if err := store.Save(e); err != nil {
		t.Fatal(err)
	}

	results, err := store.Query("pattern", "go", []string{"middleware"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Topic != "Go HTTP middleware ordering" {
		t.Fatalf("wrong topic: %s", results[0].Topic)
	}
}

func TestQueryFiltering(t *testing.T) {
	pub, priv := testKey(t)
	store := NewMemStore()

	e1 := testEntry(pub, priv)
	e1.ID = "k-001"
	e1.Language = "go"
	store.Save(e1)

	e2 := &Entry{
		ID: "k-002", Category: "pattern", Topic: "Ada SPARK contracts",
		Content: "Use Pre/Post aspects...", Language: "ada",
		Confidence: 0.9, SourceJob: "forge_456", SourceFactory: "https://ada.factory",
		Contributors: 2, ProofChainHash: HashProofChain("[]"),
	}
	SignEntry(e2, priv, pub)
	store.Save(e2)

	// Filter by language
	goResults, _ := store.Query("", "go", nil, 10)
	if len(goResults) != 1 {
		t.Fatalf("expected 1 go result, got %d", len(goResults))
	}

	adaResults, _ := store.Query("", "ada", nil, 10)
	if len(adaResults) != 1 {
		t.Fatalf("expected 1 ada result, got %d", len(adaResults))
	}

	// All results
	all, _ := store.Query("", "", nil, 10)
	if len(all) != 2 {
		t.Fatalf("expected 2 total results, got %d", len(all))
	}
}

func TestConfidenceUpdate(t *testing.T) {
	pub, priv := testKey(t)
	store := NewMemStore()
	e := testEntry(pub, priv)
	store.Save(e)

	store.UpdateConfidence("k-001", 0.1)
	got, _ := store.Get("k-001")
	if got.Confidence != 0.95 {
		t.Fatalf("expected 0.95, got %f", got.Confidence)
	}
	if got.UsedCount != 1 {
		t.Fatalf("expected used_count 1, got %d", got.UsedCount)
	}

	// Clamp at 1.0
	store.UpdateConfidence("k-001", 0.5)
	got, _ = store.Get("k-001")
	if got.Confidence != 1.0 {
		t.Fatalf("expected 1.0 (clamped), got %f", got.Confidence)
	}
}

func TestPrune(t *testing.T) {
	pub, priv := testKey(t)
	store := NewMemStore()

	e1 := testEntry(pub, priv)
	e1.ID = "k-good"
	e1.Confidence = 0.8
	store.Save(e1)

	e2 := &Entry{
		ID: "k-bad", Category: "lesson", Topic: "bad advice",
		Content: "this turned out wrong", Language: "go",
		Confidence: 0.1, SourceJob: "forge_789", SourceFactory: "https://bad.factory",
		Contributors: 1, ProofChainHash: HashProofChain("[]"),
	}
	SignEntry(e2, priv, pub)
	store.Save(e2)

	pruned, _ := store.Prune(0.2)
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}
	if store.Count() != 1 {
		t.Fatalf("expected 1 remaining, got %d", store.Count())
	}
}

func TestMergeFromMultipleFactories(t *testing.T) {
	pub, priv := testKey(t)
	store := NewMemStore()

	e1 := testEntry(pub, priv)
	e1.SourceFactory = "https://factory-a.com"
	e1.Contributors = 1
	store.Save(e1)

	e2 := testEntry(pub, priv)
	e2.ID = "k-002"
	e2.SourceFactory = "https://factory-b.cn"
	e2.Contributors = 1
	store.Save(e2)

	// Should have merged — same topic/category/language
	got, _ := store.Get("k-001")
	if got.Contributors != 2 {
		t.Fatalf("expected 2 contributors after merge, got %d", got.Contributors)
	}
}

func TestValidateContribution(t *testing.T) {
	pub, priv := testKey(t)

	// Valid
	e := testEntry(pub, priv)
	if err := ValidateContribution(e); err != nil {
		t.Fatalf("valid contribution should pass: %v", err)
	}

	// Missing proof chain
	e2 := testEntry(pub, priv)
	e2.ProofChainHash = ""
	SignEntry(e2, priv, pub)
	if err := ValidateContribution(e2); err == nil {
		t.Fatal("should reject entry without proof chain")
	}

	// Bad category
	e3 := testEntry(pub, priv)
	e3.Category = "nonsense"
	SignEntry(e3, priv, pub)
	if err := ValidateContribution(e3); err == nil {
		t.Fatal("should reject invalid category")
	}
}

func TestMarshalForAgent(t *testing.T) {
	entries := []Entry{
		{Topic: "Auth ordering", Category: "pattern", Confidence: 0.9, Contributors: 3, Content: "Auth before rate limit."},
	}
	output := MarshalForAgent(entries)
	if output == "" {
		t.Fatal("expected non-empty agent prompt")
	}
	if len(output) < 20 {
		t.Fatal("agent prompt too short")
	}
}
