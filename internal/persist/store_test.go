package persist

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ── Factory Node Tests ──────────────────────────────────────────────────────

func TestSaveAndGetNode(t *testing.T) {
	s := tempDB(t)

	node := &FactoryNode{
		PublicKey:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Handle:       "test-forge",
		Port:         8090,
		Status:       "idle",
		Models:       []string{"gemma-4-26b"},
		RegisteredAt: "2026-04-14T00:00:00Z",
		LastSeen:     "2026-04-14T00:00:00Z",
	}

	if err := s.SaveNode(node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	got, err := s.GetNode(node.PublicKey)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got == nil {
		t.Fatal("GetNode returned nil")
	}
	if got.Handle != "test-forge" {
		t.Errorf("Handle = %q, want %q", got.Handle, "test-forge")
	}
	if got.Port != 8090 {
		t.Errorf("Port = %d, want 8090", got.Port)
	}
	if len(got.Models) != 1 || got.Models[0] != "gemma-4-26b" {
		t.Errorf("Models = %v, want [gemma-4-26b]", got.Models)
	}
}

func TestGetNode_NotFound(t *testing.T) {
	s := tempDB(t)

	got, err := s.GetNode("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent node, got %+v", got)
	}
}

func TestSaveNode_Upsert(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	node := &FactoryNode{
		PublicKey:     pk,
		Handle:       "original",
		Status:       "idle",
		RegisteredAt: "2026-04-14T00:00:00Z",
		LastSeen:     "2026-04-14T00:00:00Z",
	}
	if err := s.SaveNode(node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	// Update
	node.Handle = "updated"
	node.Status = "busy"
	node.LastSeen = "2026-04-14T01:00:00Z"
	if err := s.SaveNode(node); err != nil {
		t.Fatalf("SaveNode update: %v", err)
	}

	got, _ := s.GetNode(pk)
	if got.Handle != "updated" {
		t.Errorf("Handle = %q, want %q", got.Handle, "updated")
	}
	if got.Status != "busy" {
		t.Errorf("Status = %q, want %q", got.Status, "busy")
	}
}

func TestAllNodes(t *testing.T) {
	s := tempDB(t)

	for i, pk := range []string{
		"1111111111111111111111111111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222222222222222222222222222",
	} {
		node := &FactoryNode{
			PublicKey:     pk,
			Handle:       "node-" + string(rune('A'+i)),
			Status:       "idle",
			RegisteredAt: "2026-04-14T00:00:00Z",
			LastSeen:     "2026-04-14T00:00:00Z",
		}
		if err := s.SaveNode(node); err != nil {
			t.Fatalf("SaveNode: %v", err)
		}
	}

	nodes, err := s.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("AllNodes count = %d, want 2", len(nodes))
	}
}

func TestNodeCount(t *testing.T) {
	s := tempDB(t)

	count, err := s.NodeCount()
	if err != nil {
		t.Fatalf("NodeCount: %v", err)
	}
	if count != 0 {
		t.Errorf("NodeCount = %d, want 0", count)
	}

	s.SaveNode(&FactoryNode{
		PublicKey:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		RegisteredAt: "2026-04-14T00:00:00Z",
		LastSeen:     "2026-04-14T00:00:00Z",
	})

	count, _ = s.NodeCount()
	if count != 1 {
		t.Errorf("NodeCount = %d, want 1", count)
	}
}

func TestDeleteNode(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.SaveNode(&FactoryNode{
		PublicKey:     pk,
		RegisteredAt: "2026-04-14T00:00:00Z",
		LastSeen:     "2026-04-14T00:00:00Z",
	})

	if err := s.DeleteNode(pk); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}

	got, _ := s.GetNode(pk)
	if got != nil {
		t.Error("node still exists after delete")
	}
}

// ── Balance Tests ───────────────────────────────────────────────────────────

func TestBalance_DefaultZero(t *testing.T) {
	s := tempDB(t)

	bal, err := s.GetBalance("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal != 0 {
		t.Errorf("balance = %d, want 0", bal)
	}
}

func TestSetAndGetBalance(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	if err := s.SetBalance(pk, 1000, "grant", "early adopter"); err != nil {
		t.Fatalf("SetBalance: %v", err)
	}

	bal, err := s.GetBalance(pk)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal != 1000 {
		t.Errorf("balance = %d, want 1000", bal)
	}
}

func TestAdjustBalance_Earn(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.SetBalance(pk, 1000, "grant", "init")

	newBal, err := s.AdjustBalance(pk, 500, "earn", "completed job-123")
	if err != nil {
		t.Fatalf("AdjustBalance: %v", err)
	}
	if newBal != 1500 {
		t.Errorf("balance = %d, want 1500", newBal)
	}
}

func TestAdjustBalance_Spend(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.SetBalance(pk, 1000, "grant", "init")

	newBal, err := s.AdjustBalance(pk, -300, "spend", "build request")
	if err != nil {
		t.Fatalf("AdjustBalance: %v", err)
	}
	if newBal != 700 {
		t.Errorf("balance = %d, want 700", newBal)
	}
}

func TestAdjustBalance_InsufficientFunds(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.SetBalance(pk, 100, "grant", "init")

	_, err := s.AdjustBalance(pk, -500, "spend", "too much")
	if err == nil {
		t.Fatal("expected error for insufficient funds")
	}
}

func TestTotalTokens(t *testing.T) {
	s := tempDB(t)

	s.SetBalance("1111111111111111111111111111111111111111111111111111111111111111", 1000, "grant", "init")
	s.SetBalance("2222222222222222222222222222222222222222222222222222222222222222", 2000, "grant", "init")

	total, err := s.TotalTokens()
	if err != nil {
		t.Fatalf("TotalTokens: %v", err)
	}
	if total != 3000 {
		t.Errorf("total = %d, want 3000", total)
	}
}

// ── Reputation Tests ────────────────────────────────────────────────────────

func TestSaveAndGetReputation(t *testing.T) {
	s := tempDB(t)

	r := &Reputation{
		Maker:        "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Score:        0.85,
		TotalLoans:   10,
		SuccessfulRepays: 9,
		Defaults:     1,
		TotalDelivered: 5000,
		Penalty:      0.05,
		RegisteredAt: "2026-04-01T00:00:00Z",
	}

	if err := s.SaveReputation(r); err != nil {
		t.Fatalf("SaveReputation: %v", err)
	}

	got, err := s.GetReputation(r.Maker)
	if err != nil {
		t.Fatalf("GetReputation: %v", err)
	}
	if got == nil {
		t.Fatal("GetReputation returned nil")
	}
	if got.Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", got.Score)
	}
	if got.TotalLoans != 10 {
		t.Errorf("TotalLoans = %d, want 10", got.TotalLoans)
	}
}

func TestGetReputation_NotFound(t *testing.T) {
	s := tempDB(t)

	got, err := s.GetReputation("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetReputation: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent reputation")
	}
}

// ── Token Event Audit Trail Tests ───────────────────────────────────────────

func TestTokenEventAuditTrail(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.SetBalance(pk, 1000, "grant", "early adopter")
	s.AdjustBalance(pk, 500, "earn", "job-001")
	s.AdjustBalance(pk, -200, "spend", "build-001")

	events, err := s.RecentEvents(pk, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events count = %d, want 3", len(events))
	}

	// Most recent first
	if events[0].EventType != "spend" {
		t.Errorf("events[0].EventType = %q, want spend", events[0].EventType)
	}
	if events[0].Amount != -200 {
		t.Errorf("events[0].Amount = %d, want -200", events[0].Amount)
	}
	if events[1].EventType != "earn" {
		t.Errorf("events[1].EventType = %q, want earn", events[1].EventType)
	}
	if events[2].EventType != "grant" {
		t.Errorf("events[2].EventType = %q, want grant", events[2].EventType)
	}
}

func TestRecentEvents_AllKeys(t *testing.T) {
	s := tempDB(t)

	s.SetBalance("1111111111111111111111111111111111111111111111111111111111111111", 100, "grant", "a")
	s.SetBalance("2222222222222222222222222222222222222222222222222222222222222222", 200, "grant", "b")

	events, err := s.RecentEvents("", 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events count = %d, want 2", len(events))
	}
}

// ── Persistence Across Reopen ───────────────────────────────────────────────

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	// First open: write data
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s1.SaveNode(&FactoryNode{
		PublicKey:     pk,
		Handle:       "survivor",
		Status:       "idle",
		RegisteredAt: "2026-04-14T00:00:00Z",
		LastSeen:     "2026-04-14T00:00:00Z",
	})
	s1.SetBalance(pk, 1000, "grant", "early adopter")
	s1.SaveReputation(&Reputation{
		Maker:        pk,
		Score:        0.5,
		RegisteredAt: "2026-04-14T00:00:00Z",
	})
	s1.Close()

	// Second open: verify data survived
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer s2.Close()

	node, _ := s2.GetNode(pk)
	if node == nil {
		t.Fatal("node lost after reopen")
	}
	if node.Handle != "survivor" {
		t.Errorf("Handle = %q, want survivor", node.Handle)
	}

	bal, _ := s2.GetBalance(pk)
	if bal != 1000 {
		t.Errorf("balance = %d, want 1000", bal)
	}

	rep, _ := s2.GetReputation(pk)
	if rep == nil {
		t.Fatal("reputation lost after reopen")
	}
	if rep.Score != 0.5 {
		t.Errorf("Score = %f, want 0.5", rep.Score)
	}
}

// ── Stats ───────────────────────────────────────────────────────────────────

func TestStats(t *testing.T) {
	s := tempDB(t)

	pk := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	s.SaveNode(&FactoryNode{
		PublicKey:     pk,
		RegisteredAt: "2026-04-14T00:00:00Z",
		LastSeen:     "2026-04-14T00:00:00Z",
	})
	s.SetBalance(pk, 500, "grant", "test")

	totalTokens, nodeCount, eventCount, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if totalTokens != 500 {
		t.Errorf("totalTokens = %d, want 500", totalTokens)
	}
	if nodeCount != 1 {
		t.Errorf("nodeCount = %d, want 1", nodeCount)
	}
	if eventCount != 1 {
		t.Errorf("eventCount = %d, want 1", eventCount)
	}
}

// ── Edge Cases ──────────────────────────────────────────────────────────────

func TestSanitizeHandle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal", "normal"},
		{"  padded  ", "padded"},
		{string(make([]byte, 100)), string(make([]byte, 64))},
	}
	for _, tt := range tests {
		got := SanitizeHandle(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeHandle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestOpenInvalidPath(t *testing.T) {
	_, err := Open("/nonexistent/path/to/db")
	// SQLite may create the file or fail depending on permissions
	// We mainly check it doesn't panic
	if err != nil {
		// Expected — can't create in nonexistent dir
		_ = err
	}
}

func TestEmptyDBFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.db")
	os.WriteFile(path, []byte{}, 0644)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open empty db: %v", err)
	}
	defer s.Close()

	// Should work fine — migration creates tables
	count, _ := s.NodeCount()
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
