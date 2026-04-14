package persist

import (
	"testing"
	"time"
)

func TestSubmitAndGetBuild(t *testing.T) {
	s := openTestDB(t)

	b := &Build{
		ID:          GenerateBuildID(),
		PublicKey:   "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233",
		Spec:        "Purpose: A CLI tool that counts words in files.\n\nFeatures:\n1. Count words\n2. Count lines\n\nLanguage: Go",
		Destination: "network",
		Status:      BuildQueued,
		Cost:        15,
		Language:    "go",
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.SubmitBuild(b); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetBuild(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected build, got nil")
	}
	if got.Status != BuildQueued {
		t.Errorf("expected queued, got %s", got.Status)
	}
	if got.Cost != 15 {
		t.Errorf("expected cost 15, got %d", got.Cost)
	}
	if got.Language != "go" {
		t.Errorf("expected go, got %s", got.Language)
	}
}

func TestGetBuild_NotFound(t *testing.T) {
	s := openTestDB(t)

	got, err := s.GetBuild("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for non-existent build")
	}
}

func TestUpdateBuildStatus(t *testing.T) {
	s := openTestDB(t)

	b := &Build{
		ID: GenerateBuildID(), Spec: "test spec", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.SubmitBuild(b)

	if err := s.UpdateBuildStatus(b.ID, BuildComplete, "success: all tests pass"); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetBuild(b.ID)
	if got.Status != BuildComplete {
		t.Errorf("expected complete, got %s", got.Status)
	}
	if got.Result != "success: all tests pass" {
		t.Errorf("expected result, got %s", got.Result)
	}
}

func TestAssignBuild(t *testing.T) {
	s := openTestDB(t)

	b := &Build{
		ID: GenerateBuildID(), Spec: "test spec", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.SubmitBuild(b)

	factory := "ff11223344556677ff11223344556677ff11223344556677ff11223344556677"
	if err := s.AssignBuild(b.ID, factory); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetBuild(b.ID)
	if got.Status != BuildAssigned {
		t.Errorf("expected assigned, got %s", got.Status)
	}
	if got.AssignedTo != factory {
		t.Error("expected factory assignment")
	}
}

func TestAssignBuild_OnlyQueued(t *testing.T) {
	s := openTestDB(t)

	b := &Build{
		ID: GenerateBuildID(), Spec: "test", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.SubmitBuild(b)

	// Complete it first
	s.UpdateBuildStatus(b.ID, BuildComplete, "done")

	// Try to assign a completed build — should have no effect
	factory := "ff11223344556677ff11223344556677ff11223344556677ff11223344556677"
	s.AssignBuild(b.ID, factory)

	got, _ := s.GetBuild(b.ID)
	if got.Status != BuildComplete {
		t.Errorf("completed build should not be re-assigned, got %s", got.Status)
	}
}

func TestNextQueuedBuild(t *testing.T) {
	s := openTestDB(t)

	// Empty queue
	got, err := s.NextQueuedBuild()
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for empty queue")
	}

	// Add two builds
	b1 := &Build{
		ID: GenerateBuildID(), Spec: "first", Status: BuildQueued,
		SubmittedAt: "2026-04-14T10:00:00Z",
		UpdatedAt:   "2026-04-14T10:00:00Z",
	}
	b2 := &Build{
		ID: GenerateBuildID(), Spec: "second", Status: BuildQueued,
		SubmittedAt: "2026-04-14T10:01:00Z",
		UpdatedAt:   "2026-04-14T10:01:00Z",
	}
	s.SubmitBuild(b1)
	s.SubmitBuild(b2)

	// Should get the oldest
	got, _ = s.NextQueuedBuild()
	if got == nil || got.ID != b1.ID {
		t.Error("expected oldest queued build first")
	}
}

func TestQueuedBuilds(t *testing.T) {
	s := openTestDB(t)

	for i := 0; i < 3; i++ {
		s.SubmitBuild(&Build{
			ID: GenerateBuildID(), Spec: "spec", Status: BuildQueued,
			SubmittedAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}

	builds, err := s.QueuedBuilds()
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 3 {
		t.Errorf("expected 3 queued, got %d", len(builds))
	}
}

func TestBuildsBySubmitter(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	for i := 0; i < 5; i++ {
		s.SubmitBuild(&Build{
			ID: GenerateBuildID(), PublicKey: pk, Spec: "spec", Status: BuildQueued,
			SubmittedAt: time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}

	builds, _ := s.BuildsBySubmitter(pk, 3)
	if len(builds) != 3 {
		t.Errorf("expected 3 (limited), got %d", len(builds))
	}
}

func TestBuildStats(t *testing.T) {
	s := openTestDB(t)

	s.SubmitBuild(&Build{ID: "q1", Spec: "s", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339)})
	s.SubmitBuild(&Build{ID: "q2", Spec: "s", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339)})
	s.SubmitBuild(&Build{ID: "r1", Spec: "s", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339)})
	s.UpdateBuildStatus("r1", BuildRunning, "")
	s.SubmitBuild(&Build{ID: "c1", Spec: "s", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339)})
	s.UpdateBuildStatus("c1", BuildComplete, "done")

	queued, running, complete, failed, _ := s.BuildStats()
	if queued != 2 {
		t.Errorf("expected 2 queued, got %d", queued)
	}
	if running != 1 {
		t.Errorf("expected 1 running, got %d", running)
	}
	if complete != 1 {
		t.Errorf("expected 1 complete, got %d", complete)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed, got %d", failed)
	}
}

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name     string
		specLen  int
		expected int64
	}{
		{"tiny", 50, 10},
		{"small", 500, 15},
		{"medium", 1500, 25},
		{"large", 5000, 60},
		{"huge", 30000, 200}, // capped
	}
	for _, tt := range tests {
		spec := make([]byte, tt.specLen)
		for i := range spec {
			spec[i] = 'a'
		}
		cost := EstimateCost(string(spec))
		if cost != tt.expected {
			t.Errorf("%s: expected ◎%d, got ◎%d", tt.name, tt.expected, cost)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		spec string
		lang string
	}{
		{"A CLI tool in Go that counts words", "go"},
		{"Build an Ada package using SPARK subset", "ada"},
		{"Write a GNAT project", "ada"},
		{"A simple web server", "go"},
	}
	for _, tt := range tests {
		if got := DetectLanguage(tt.spec); got != tt.lang {
			t.Errorf("DetectLanguage(%q) = %s, want %s", tt.spec[:30], got, tt.lang)
		}
	}
}

func TestGenerateBuildID(t *testing.T) {
	id1 := GenerateBuildID()
	id2 := GenerateBuildID()
	if len(id1) != 16 {
		t.Errorf("expected 16 chars, got %d", len(id1))
	}
	if id1 == id2 {
		t.Error("IDs should be unique")
	}
}

func TestPurgeBuild(t *testing.T) {
	s := openTestDB(t)

	b := &Build{
		ID: GenerateBuildID(), Spec: "to delete", Status: BuildQueued,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	s.SubmitBuild(b)

	if err := s.PurgeBuild(b.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetBuild(b.ID)
	if got != nil {
		t.Error("expected nil after purge")
	}

	// Purge non-existent
	if err := s.PurgeBuild("nonexistent"); err == nil {
		t.Error("expected error for non-existent build")
	}
}
