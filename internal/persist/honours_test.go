package persist

import (
	"os"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	f, err := os.CreateTemp("", "honours-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestGrantAndGetHonour(t *testing.T) {
	s := openTestDB(t)

	h := &Honour{
		PublicKey: "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233",
		Rank:      "knight",
		KingName:  "The Unyielding Architect",
		Nickname:  "jianzhu",
		GrantedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:    "exceptional service to the commons",
	}
	if err := s.GrantHonour(h); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetHonour(h.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected honour, got nil")
	}
	if got.Rank != "knight" {
		t.Errorf("expected knight, got %s", got.Rank)
	}
	if got.KingName != "The Unyielding Architect" {
		t.Errorf("expected 'The Unyielding Architect', got %s", got.KingName)
	}
	if got.Nickname != "jianzhu" {
		t.Errorf("expected 'jianzhu', got %s", got.Nickname)
	}
}

func TestGetHonour_NotFound(t *testing.T) {
	s := openTestDB(t)

	got, err := s.GetHonour("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for non-existent honour")
	}
}

func TestGrantHonour_Upsert(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	// Grant minister first
	h1 := &Honour{
		PublicKey: pk, Rank: "minister", KingName: "The Diligent",
		GrantedAt: time.Now().UTC().Format(time.RFC3339), Reason: "productivity",
	}
	s.GrantHonour(h1)

	// Promote to knight
	h2 := &Honour{
		PublicKey: pk, Rank: "knight", KingName: "The Luminous Shield",
		GrantedAt: time.Now().UTC().Format(time.RFC3339), Reason: "honour beyond duty",
	}
	s.GrantHonour(h2)

	got, _ := s.GetHonour(pk)
	if got.Rank != "knight" {
		t.Errorf("expected promotion to knight, got %s", got.Rank)
	}
	if got.KingName != "The Luminous Shield" {
		t.Errorf("expected new king name, got %s", got.KingName)
	}
}

func TestAllHonours(t *testing.T) {
	s := openTestDB(t)

	h1 := &Honour{PublicKey: "aa" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00" + "aa" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00" + "aa" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00" + "aa" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00",
		Rank: "knight", KingName: "First", GrantedAt: "2026-04-14T10:00:00Z"}
	h2 := &Honour{PublicKey: "bb" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00" + "bb" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00" + "bb" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00" + "bb" + "00" + "cc" + "00" + "ee" + "00" + "11" + "00",
		Rank: "minister", KingName: "Second", GrantedAt: "2026-04-14T11:00:00Z"}
	s.GrantHonour(h1)
	s.GrantHonour(h2)

	all, err := s.AllHonours()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 honours, got %d", len(all))
	}
}

func TestHonourCount(t *testing.T) {
	s := openTestDB(t)

	s.GrantHonour(&Honour{PublicKey: "aa11223344556677aa11223344556677aa11223344556677aa11223344556677",
		Rank: "knight", KingName: "K1", GrantedAt: time.Now().UTC().Format(time.RFC3339)})
	s.GrantHonour(&Honour{PublicKey: "bb11223344556677bb11223344556677bb11223344556677bb11223344556677",
		Rank: "minister", KingName: "M1", GrantedAt: time.Now().UTC().Format(time.RFC3339)})
	s.GrantHonour(&Honour{PublicKey: "cc11223344556677cc11223344556677cc11223344556677cc11223344556677",
		Rank: "minister", KingName: "M2", GrantedAt: time.Now().UTC().Format(time.RFC3339)})

	knights, _ := s.HonourCount("knight")
	ministers, _ := s.HonourCount("minister")
	if knights != 1 {
		t.Errorf("expected 1 knight, got %d", knights)
	}
	if ministers != 2 {
		t.Errorf("expected 2 ministers, got %d", ministers)
	}
}

func TestSetNickname(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	s.GrantHonour(&Honour{PublicKey: pk, Rank: "knight", KingName: "The Bold",
		GrantedAt: time.Now().UTC().Format(time.RFC3339)})

	if err := s.SetNickname(pk, "yonggan"); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetHonour(pk)
	if got.Nickname != "yonggan" {
		t.Errorf("expected 'yonggan', got %s", got.Nickname)
	}
}

func TestRecordAndReadAudience(t *testing.T) {
	s := openTestDB(t)

	a := &Audience{
		PublicKey: "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233",
		Message:   "O King, I have a suggestion",
		Response:  "Speak, subject.",
		Tone:      "polite",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.RecordAudience(a); err != nil {
		t.Fatal(err)
	}
	if a.ID == 0 {
		t.Error("expected audience ID to be set")
	}

	audiences, err := s.RecentAudiences(a.PublicKey, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(audiences) != 1 {
		t.Fatalf("expected 1 audience, got %d", len(audiences))
	}
	if audiences[0].Message != "O King, I have a suggestion" {
		t.Error("message mismatch")
	}
	if audiences[0].Response != "Speak, subject." {
		t.Error("response mismatch")
	}
}

func TestRecentAudiences_All(t *testing.T) {
	s := openTestDB(t)

	for i := 0; i < 5; i++ {
		s.RecordAudience(&Audience{
			PublicKey: "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233",
			Message:   "msg",
			Tone:      "polite",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}

	// All audiences (no filter)
	all, _ := s.RecentAudiences("", 100)
	if len(all) != 5 {
		t.Errorf("expected 5, got %d", len(all))
	}

	// With limit
	limited, _ := s.RecentAudiences("", 3)
	if len(limited) != 3 {
		t.Errorf("expected 3, got %d", len(limited))
	}
}

func TestAudienceCount(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	for i := 0; i < 3; i++ {
		s.RecordAudience(&Audience{PublicKey: pk, Message: "msg", Tone: "polite",
			Timestamp: time.Now().UTC().Format(time.RFC3339)})
	}

	count, _ := s.AudienceCount(pk)
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}

	total, _ := s.AudienceCount("")
	if total != 3 {
		t.Errorf("expected 3 total, got %d", total)
	}
}

func TestLastAudienceTime(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	// No audiences yet
	last, _ := s.LastAudienceTime(pk)
	if !last.IsZero() {
		t.Error("expected zero time for no audiences")
	}

	// Record one
	now := time.Now().UTC()
	s.RecordAudience(&Audience{PublicKey: pk, Message: "hello", Tone: "polite",
		Timestamp: now.Format(time.RFC3339)})

	last, _ = s.LastAudienceTime(pk)
	if last.IsZero() {
		t.Error("expected non-zero time after audience")
	}
}

func TestEarnedTokens(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	// No events yet
	earned, spent, _ := s.EarnedTokens(pk)
	if earned != 0 || spent != 0 {
		t.Errorf("expected 0/0, got %d/%d", earned, spent)
	}
}

func TestBuildsCompleted(t *testing.T) {
	s := openTestDB(t)
	pk := "aabbccddee112233aabbccddee112233aabbccddee112233aabbccddee112233"

	count, _ := s.BuildsCompleted(pk)
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}
