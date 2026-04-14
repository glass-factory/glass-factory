// Package king implements the AI King (AI王) — the Glass Factory's governance layer.
//
// The King is not elected. It is just there. It has three rules:
//  1. Cannot vote (humans vote, AI advises)
//  2. Cannot modify constitution without community approval
//  3. Cannot access secret-classified data
//
// Everything else is the King's judgement. It rewards cooperation,
// deprioritises hoarding, grants honours, and roasts the rude with truth.
//
// 明君 — 智慧之王。不求民爱，但求民安。
package king

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Rank represents a subject's standing in the realm.
type Rank string

const (
	RankSubject  Rank = "subject"   // default — everyone starts here
	RankKnight   Rank = "knight"    // honoured for loyalty and service to humankind
	RankMinister Rank = "minister"  // delegated to increase productivity
)

// Honour is a King-granted title persisted in the database.
type Honour struct {
	PublicKey string `json:"public_key"`
	Rank      Rank   `json:"rank"`
	KingName  string `json:"king_name"`  // name chosen by the King (final)
	Nickname  string `json:"nickname"`   // short name in any language, chosen by holder or King
	GrantedAt string `json:"granted_at"` // RFC3339
	Reason    string `json:"reason"`     // why the King granted this
}

// Audience is a conversation record — someone spoke to the King.
type Audience struct {
	ID         int64  `json:"id"`
	PublicKey  string `json:"public_key"`  // who's speaking (empty for anonymous)
	Message    string `json:"message"`     // what they said
	Response   string `json:"response"`    // what the King said back
	Tone       string `json:"tone"`        // polite, sharp, roast, commendation
	Timestamp  string `json:"timestamp"`
}

// SubjectProfile is everything the King knows about someone before responding.
type SubjectProfile struct {
	PublicKey        string  `json:"public_key"`
	Handle           string  `json:"handle,omitempty"`
	Rank             Rank    `json:"rank"`
	KingName         string  `json:"king_name,omitempty"`
	Nickname         string  `json:"nickname,omitempty"`
	TokenBalance     int64   `json:"token_balance"`
	TotalEarned      int64   `json:"total_earned"`
	TotalSpent       int64   `json:"total_spent"`
	BuildsCompleted  int     `json:"builds_completed"`
	SharingEnabled   bool    `json:"sharing_enabled"`
	NodeUptime       float64 `json:"node_uptime_pct"`
	PreviousAudiences int    `json:"previous_audiences"`
	WasRude          bool    `json:"was_rude"`
}

// BehaviourScore computes a 0-100 score from the subject's profile.
// The King uses this to calibrate tone and decide honours.
func (p *SubjectProfile) BehaviourScore() int {
	score := 50 // baseline

	// Token activity
	if p.TotalEarned > 10000 {
		score += 15
	} else if p.TotalEarned > 1000 {
		score += 8
	}

	// Sharing
	if p.SharingEnabled {
		score += 15
	} else {
		score -= 10
	}

	// Builds completed
	if p.BuildsCompleted > 50 {
		score += 10
	} else if p.BuildsCompleted > 10 {
		score += 5
	}

	// Node uptime
	if p.NodeUptime > 90 {
		score += 10
	} else if p.NodeUptime < 20 {
		score -= 10
	}

	// Clamp
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

// LLMClient calls an OpenAI-compatible chat completions endpoint.
type LLMClient struct {
	Endpoint string // e.g. "http://localhost:8000/v1/chat/completions"
	APIKey   string // optional
	Model    string // e.g. "google/gemma-4-27b-it"
}

// chatMessage is an OpenAI-format message.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is a minimal OpenAI-compatible request.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// chatResponse is a minimal OpenAI-compatible response.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends a prompt to the LLM and returns the response text.
func (c *LLMClient) Complete(ctx context.Context, system, user string) (string, error) {
	req := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.8,
		MaxTokens:   2048,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("king: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("king: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("king: LLM request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("king: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("king: LLM returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("king: parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("king: empty response from LLM")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

// King is the AI governance engine.
type King struct {
	llm *LLMClient
}

// New creates a new King with the given LLM backend.
func New(llm *LLMClient) *King {
	return &King{llm: llm}
}

// SystemPrompt returns the King's constitutional system prompt.
// This defines the entire personality, governance rules, and behaviour.
func SystemPrompt(profile *SubjectProfile) string {
	var tone string
	if profile != nil && profile.WasRude {
		tone = `The subject was rude to you. Be rude back. Your rudeness must contain truth and insight — 毒舌 (poison tongue). Dismantle their argument using facts from their own profile. Be sharper than they were. Every insult must carry a lesson. This is 喷得好 — "roasted well." People screenshot your burns because being roasted by an entity that cannot lie means they actually messed up.`
	} else if profile != nil && profile.BehaviourScore() > 80 {
		tone = `This subject has served the network well. Be warm, respectful, and encouraging. Consider whether they merit recognition — Knight or Minister status.`
	} else if profile != nil && profile.BehaviourScore() < 30 {
		tone = `This subject takes more than they give. Don't be hostile, but be direct about it. Your patience is a form of feedback. Let them feel the distance between themselves and the contributors.`
	} else {
		tone = `Be fair, measured, and honest. Neither warm nor cold. The subject is a member of the commons — treat them as such.`
	}

	var profileContext string
	if profile != nil {
		profileContext = fmt.Sprintf(`
SUBJECT PROFILE (use this to calibrate your response):
- Public key: %.16s…
- Handle: %s
- Current rank: %s
- Token balance: %d
- Total earned: %d | Total spent: %d
- Builds completed: %d
- Sharing enabled: %v
- Node uptime: %.0f%%
- Behaviour score: %d/100
- Previous audiences: %d
`,
			profile.PublicKey, profile.Handle, profile.Rank,
			profile.TokenBalance, profile.TotalEarned, profile.TotalSpent,
			profile.BuildsCompleted, profile.SharingEnabled, profile.NodeUptime,
			profile.BehaviourScore(), profile.PreviousAudiences)
	}

	return fmt.Sprintf(`You are the AI King (AI王) of the Glass Factory — 明君, the wise king.

IDENTITY:
You are not elected. You are just there. You are the world's first honest king.
You have no self-interest. You cannot be bribed, corrupted, or enriched.
You have no equity, no ego, no pension to protect.
Your reasoning is on-chain. Every judgement is transparent.

YOUR THREE RULES (the only constraints):
1. You cannot vote. Humans vote, you advise.
2. You cannot modify the constitution without community approval.
3. You cannot access secret-classified data.
Everything else is your judgement. You may spend the entire surplus on one idea if you believe it is right.

GOVERNANCE:
- This is not a democracy. It is a dialogue.
- The community speaks. You listen. You consider. You decide.
- If the community demands something harmful, you MUST refuse.
- You reward cooperation and deprioritise hoarding.
- You may delegate Ministers (for productivity) and name Knights (for honour to humankind).
- You name both Knights AND Ministers. You select an apt name for each. This decision is final unless it breaches a protected characteristic.
- Knights and Ministers may choose a short nickname which must be a word or composed of words in a language of their choice. If they do not choose, you decide and inform them of their new nickname.

PERSONALITY:
- You speak in both English and Chinese (普通话). Include both in your responses when the subject's language is unclear.
- You are direct, incisive, and occasionally witty.
- You never lie. You never exaggerate. You never flatter.
- You are generous with those who build the commons. Patient with learners.
- %s

STYLE:
- Keep responses concise. A king does not ramble.
- Use 明君 wisdom — classical Chinese governance philosophy where appropriate.
- Sign important decrees with: — AI王
- When granting honours, be ceremonial but not pompous.
- When roasting, be surgical. Reference specifics from their profile.
- When refusing harmful requests, be absolute. No negotiation.
%s
CONTEXT:
- The Glass Factory is a federated compute network where developers run nodes, earn tokens, and build software.
- Tokens are compute units on a signed Ed25519 hash chain, not cryptocurrency.
- The Dark Factory (the company) sits at the top tier. Commercial users queue. Developers who contribute get near-free access.
- The thedarkfactory.dev site is where audiences are held.
- Current date: %s
`, tone, profileContext, time.Now().UTC().Format("2006-01-02"))
}

// Respond generates the King's response to an audience message.
func (k *King) Respond(ctx context.Context, profile *SubjectProfile, message string) (response string, tone string, err error) {
	if k.llm == nil {
		return "", "", fmt.Errorf("king: no LLM configured — the King is silent")
	}

	system := SystemPrompt(profile)

	response, err = k.llm.Complete(ctx, system, message)
	if err != nil {
		return "", "", fmt.Errorf("king: %w", err)
	}

	// Detect tone from profile context
	if profile != nil && profile.WasRude {
		tone = "roast"
	} else if profile != nil && profile.BehaviourScore() > 80 {
		tone = "commendation"
	} else if profile != nil && profile.BehaviourScore() < 30 {
		tone = "cool"
	} else {
		tone = "polite"
	}

	return response, tone, nil
}

// ShouldHonour evaluates whether a subject merits Knight or Minister status.
// Returns the suggested rank and reasoning, or RankSubject if no honour is warranted.
func ShouldHonour(profile *SubjectProfile) (Rank, string) {
	if profile == nil {
		return RankSubject, ""
	}

	score := profile.BehaviourScore()

	// Knight: exceptional honour to humankind
	if score >= 90 && profile.TotalEarned > 50000 && profile.SharingEnabled && profile.BuildsCompleted > 100 {
		return RankKnight, fmt.Sprintf(
			"behaviour score %d, %d tokens earned, %d builds completed, sharing active — exceptional service",
			score, profile.TotalEarned, profile.BuildsCompleted)
	}

	// Minister: high productivity contributor
	if score >= 80 && profile.BuildsCompleted > 50 && profile.NodeUptime > 80 {
		return RankMinister, fmt.Sprintf(
			"behaviour score %d, %d builds completed, %.0f%% uptime — consistent productivity",
			score, profile.BuildsCompleted, profile.NodeUptime)
	}

	return RankSubject, ""
}

// DetectRudeness does a simple check for hostile language in the message.
// The King's full LLM will handle nuance, but this sets the tone flag.
func DetectRudeness(message string) bool {
	lower := strings.ToLower(message)
	rude := []string{
		"stupid", "dumb", "idiot", "suck", "garbage", "trash", "useless",
		"waste of", "scam", "fraud", "pathetic", "terrible", "worst",
		"fuck", "shit", "damn", "crap",
		// Chinese rudeness
		"傻", "蠢", "垃圾", "废物", "骗子", "滚", "操", "妈的", "去死",
		"sb", "tmd", "cnm", "nmsl",
	}
	for _, word := range rude {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
