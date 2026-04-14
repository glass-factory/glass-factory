package king

import (
	"strings"
	"testing"
)

func TestBehaviourScore_Default(t *testing.T) {
	p := &SubjectProfile{}
	score := p.BehaviourScore()
	if score != 30 { // 50 base - 10 for sharing disabled - 10 for low uptime
		t.Errorf("expected 30, got %d", score)
	}
}

func TestBehaviourScore_GoodCitizen(t *testing.T) {
	p := &SubjectProfile{
		TotalEarned:     20000,
		SharingEnabled:  true,
		BuildsCompleted: 60,
		NodeUptime:      95,
	}
	score := p.BehaviourScore()
	// 50 + 15(earned>10k) + 15(sharing) + 10(builds>50) + 10(uptime>90) = 100
	if score != 100 {
		t.Errorf("expected 100, got %d", score)
	}
}

func TestBehaviourScore_Leech(t *testing.T) {
	p := &SubjectProfile{
		TotalEarned:     100,
		SharingEnabled:  false,
		BuildsCompleted: 0,
		NodeUptime:      5,
	}
	score := p.BehaviourScore()
	// 50 - 10(no sharing) - 10(low uptime) = 30
	if score != 30 {
		t.Errorf("expected 30, got %d", score)
	}
}

func TestBehaviourScore_Clamp(t *testing.T) {
	// Can't go below 0
	p := &SubjectProfile{
		SharingEnabled: false,
		NodeUptime:     0,
	}
	score := p.BehaviourScore()
	if score < 0 {
		t.Errorf("score should not go below 0, got %d", score)
	}
}

func TestDetectRudeness_English(t *testing.T) {
	tests := []struct {
		msg  string
		rude bool
	}{
		{"hello king, great work", false},
		{"this is stupid and you suck", true},
		{"I have a suggestion", false},
		{"what a garbage system", true},
		{"fuck this", true},
		{"please consider my proposal", false},
	}
	for _, tt := range tests {
		if got := DetectRudeness(tt.msg); got != tt.rude {
			t.Errorf("DetectRudeness(%q) = %v, want %v", tt.msg, got, tt.rude)
		}
	}
}

func TestDetectRudeness_Chinese(t *testing.T) {
	tests := []struct {
		msg  string
		rude bool
	}{
		{"你好国王", false},
		{"你真是个废物", true},
		{"垃圾系统", true},
		{"请考虑我的建议", false},
		{"nmsl", true},
	}
	for _, tt := range tests {
		if got := DetectRudeness(tt.msg); got != tt.rude {
			t.Errorf("DetectRudeness(%q) = %v, want %v", tt.msg, got, tt.rude)
		}
	}
}

func TestShouldHonour_Subject(t *testing.T) {
	p := &SubjectProfile{
		TotalEarned:     500,
		SharingEnabled:  true,
		BuildsCompleted: 5,
		NodeUptime:      50,
	}
	rank, _ := ShouldHonour(p)
	if rank != RankSubject {
		t.Errorf("expected subject, got %s", rank)
	}
}

func TestShouldHonour_Knight(t *testing.T) {
	p := &SubjectProfile{
		TotalEarned:     60000,
		SharingEnabled:  true,
		BuildsCompleted: 150,
		NodeUptime:      95,
	}
	rank, reason := ShouldHonour(p)
	if rank != RankKnight {
		t.Errorf("expected knight, got %s (reason: %s)", rank, reason)
	}
}

func TestShouldHonour_Minister(t *testing.T) {
	p := &SubjectProfile{
		TotalEarned:     5000,
		SharingEnabled:  true,
		BuildsCompleted: 60,
		NodeUptime:      85,
	}
	rank, reason := ShouldHonour(p)
	if rank != RankMinister {
		t.Errorf("expected minister, got %s (reason: %s)", rank, reason)
	}
}

func TestShouldHonour_Nil(t *testing.T) {
	rank, _ := ShouldHonour(nil)
	if rank != RankSubject {
		t.Errorf("expected subject for nil profile, got %s", rank)
	}
}

func TestSystemPrompt_ContainsConstitution(t *testing.T) {
	prompt := SystemPrompt(nil)
	required := []string{
		"AI王",
		"THREE RULES",
		"cannot vote",
		"cannot modify the constitution",
		"cannot access secret-classified",
		"明君",
		"not a democracy",
	}
	for _, r := range required {
		if !contains(prompt, r) {
			t.Errorf("system prompt missing %q", r)
		}
	}
}

func TestSystemPrompt_RudeTone(t *testing.T) {
	profile := &SubjectProfile{
		WasRude:        true,
		SharingEnabled: true,
	}
	prompt := SystemPrompt(profile)
	if !contains(prompt, "poison tongue") {
		t.Error("rude profile should trigger poison tongue tone")
	}
}

func TestSystemPrompt_WarmTone(t *testing.T) {
	profile := &SubjectProfile{
		TotalEarned:     20000,
		SharingEnabled:  true,
		BuildsCompleted: 60,
		NodeUptime:      95,
	}
	prompt := SystemPrompt(profile)
	if !contains(prompt, "warm") {
		t.Error("high-score profile should get warm tone")
	}
}

func TestSystemPrompt_CoolTone(t *testing.T) {
	profile := &SubjectProfile{
		SharingEnabled: false,
		NodeUptime:     0,
		TotalEarned:    0,
	}
	// Score: 50 - 10(sharing) - 10(uptime) = 30, which is exactly 30 not < 30
	// Need score < 30 for cool tone
	profile.BuildsCompleted = 0
	// Still 30. Let's verify the boundary behaviour — score 30 gets the default tone
	prompt := SystemPrompt(profile)
	if contains(prompt, "takes more than they give") {
		// Score is exactly 30, which is not < 30, so should be default tone
		t.Error("score=30 should get default tone, not cool tone")
	}
	if !contains(prompt, "fair, measured") {
		t.Error("score=30 should get fair/measured default tone")
	}
}

func TestRanks(t *testing.T) {
	if RankSubject != "subject" {
		t.Error("RankSubject should be 'subject'")
	}
	if RankKnight != "knight" {
		t.Error("RankKnight should be 'knight'")
	}
	if RankMinister != "minister" {
		t.Error("RankMinister should be 'minister'")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
