package lending

import (
	"testing"
	"time"
)

func setupLedger() *Ledger {
	l := NewLedger()

	// Lender with tokens
	l.RegisterMaker("lender-aaa")
	l.SetBalance("lender-aaa", 10000)

	// Borrower with reputation
	l.RegisterMaker("borrower-bbb")
	l.SetBalance("borrower-bbb", 500)
	l.reputations["borrower-bbb"].SuccessfulRepays = 8
	l.reputations["borrower-bbb"].TotalLoans = 10
	l.reputations["borrower-bbb"].TotalDelivered = 15000
	l.reputations["borrower-bbb"].RegisteredAt = time.Now().Add(-180 * 24 * time.Hour).Format(time.RFC3339)

	return l
}

func TestLendAndBorrow(t *testing.T) {
	l := setupLedger()

	offer := &Offer{
		ID: "offer-1", Lender: "lender-aaa", Amount: 1000,
		MinReputation: 0.5, MaxDurationHrs: 168, InterestPct: 5,
	}
	if err := l.Lend(offer); err != nil {
		t.Fatalf("lend failed: %v", err)
	}
	if l.Balance("lender-aaa") != 9000 {
		t.Fatalf("lender balance should be 9000, got %d", l.Balance("lender-aaa"))
	}

	loan, err := l.Borrow("loan-1", "offer-1", "borrower-bbb", "forge job")
	if err != nil {
		t.Fatalf("borrow failed: %v", err)
	}
	if loan.RepayAmount != 1050 { // 1000 + 5%
		t.Fatalf("repay amount should be 1050, got %d", loan.RepayAmount)
	}
	if l.Balance("borrower-bbb") != 1500 { // 500 + 1000
		t.Fatalf("borrower balance should be 1500, got %d", l.Balance("borrower-bbb"))
	}
}

func TestCannotBorrowFromSelf(t *testing.T) {
	l := setupLedger()
	offer := &Offer{
		ID: "offer-1", Lender: "lender-aaa", Amount: 100,
		MinReputation: 0, MaxDurationHrs: 24, InterestPct: 0,
	}
	l.Lend(offer)

	_, err := l.Borrow("loan-1", "offer-1", "lender-aaa", "self-dealing")
	if err == nil {
		t.Fatal("should not be able to borrow from yourself")
	}
}

func TestReputationGating(t *testing.T) {
	l := NewLedger()
	l.RegisterMaker("lender-aaa")
	l.SetBalance("lender-aaa", 10000)
	l.RegisterMaker("newbie-ccc")
	// Newbie has no history — reputation 0

	offer := &Offer{
		ID: "offer-1", Lender: "lender-aaa", Amount: 100,
		MinReputation: 0.5, MaxDurationHrs: 24, InterestPct: 0,
	}
	l.Lend(offer)

	_, err := l.Borrow("loan-1", "offer-1", "newbie-ccc", "need tokens")
	if err == nil {
		t.Fatal("newbie with 0 reputation should not be able to borrow")
	}
}

func TestRepayment(t *testing.T) {
	l := setupLedger()
	offer := &Offer{
		ID: "offer-1", Lender: "lender-aaa", Amount: 1000,
		MinReputation: 0.5, MaxDurationHrs: 168, InterestPct: 5,
	}
	l.Lend(offer)
	l.Borrow("loan-1", "offer-1", "borrower-bbb", "forge job")

	// Borrower does work and earns more tokens
	l.SetBalance("borrower-bbb", 2000)

	err := l.Repay("loan-1", `[{"stage":"code","result_hash":"abc"}]`)
	if err != nil {
		t.Fatalf("repay failed: %v", err)
	}

	// Borrower paid 1050, has 950 left
	if l.Balance("borrower-bbb") != 950 {
		t.Fatalf("borrower should have 950, got %d", l.Balance("borrower-bbb"))
	}
	// Lender got back 1050 (original 1000 + 50 interest), had 9000
	if l.Balance("lender-aaa") != 10050 {
		t.Fatalf("lender should have 10050, got %d", l.Balance("lender-aaa"))
	}

	rep, _ := l.GetReputation("borrower-bbb")
	if rep.SuccessfulRepays != 9 { // was 8, now 9
		t.Fatalf("expected 9 successful repays, got %d", rep.SuccessfulRepays)
	}
}

func TestDefault(t *testing.T) {
	l := setupLedger()
	offer := &Offer{
		ID: "offer-1", Lender: "lender-aaa", Amount: 1000,
		MinReputation: 0.5, MaxDurationHrs: 1, InterestPct: 0,
	}
	l.Lend(offer)
	l.Borrow("loan-1", "offer-1", "borrower-bbb", "forge job")

	repBefore, _ := l.GetReputation("borrower-bbb")
	scoreBefore := repBefore.Score

	err := l.Default("loan-1")
	if err != nil {
		t.Fatalf("default failed: %v", err)
	}

	repAfter, _ := l.GetReputation("borrower-bbb")
	if repAfter.Score >= scoreBefore {
		t.Fatal("reputation should decrease after default")
	}
	if repAfter.Defaults != 1 {
		t.Fatalf("expected 1 default, got %d", repAfter.Defaults)
	}
}

func TestNoLeveragedLending(t *testing.T) {
	l := setupLedger()

	// Borrower creates a lending offer
	l.SetBalance("borrower-bbb", 5000)
	offer1 := &Offer{
		ID: "offer-bbb", Lender: "borrower-bbb", Amount: 2000,
		MinReputation: 0, MaxDurationHrs: 24, InterestPct: 10,
	}
	l.Lend(offer1)

	// Now try to borrow — should be blocked
	offer2 := &Offer{
		ID: "offer-lender", Lender: "lender-aaa", Amount: 500,
		MinReputation: 0, MaxDurationHrs: 24, InterestPct: 0,
	}
	l.Lend(offer2)

	_, err := l.Borrow("loan-1", "offer-lender", "borrower-bbb", "leveraged")
	if err == nil {
		t.Fatal("should not allow borrowing while you have active lending offers")
	}
}

func TestCancelOffer(t *testing.T) {
	l := setupLedger()
	offer := &Offer{
		ID: "offer-1", Lender: "lender-aaa", Amount: 1000,
		MinReputation: 0.5, MaxDurationHrs: 168, InterestPct: 5,
	}
	l.Lend(offer)

	if l.Balance("lender-aaa") != 9000 {
		t.Fatalf("expected 9000 after lend, got %d", l.Balance("lender-aaa"))
	}

	err := l.CancelOffer("offer-1", "lender-aaa")
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	if l.Balance("lender-aaa") != 10000 {
		t.Fatalf("expected 10000 after cancel, got %d", l.Balance("lender-aaa"))
	}
}

func TestReputationScore(t *testing.T) {
	r := &Reputation{
		Maker:            "test-maker",
		TotalLoans:       10,
		SuccessfulRepays: 9,
		TotalDelivered:   20000,
		RegisteredAt:     time.Now().Add(-365 * 24 * time.Hour).Format(time.RFC3339),
	}
	score := r.ComputeScore()
	if score < 0.8 || score > 1.0 {
		t.Fatalf("expected score 0.8-1.0 for good maker, got %f", score)
	}

	// New maker
	r2 := &Reputation{
		Maker:            "newbie",
		TotalLoans:       1,
		SuccessfulRepays: 1,
		TotalDelivered:   100,
		RegisteredAt:     time.Now().Add(-7 * 24 * time.Hour).Format(time.RFC3339),
	}
	score2 := r2.ComputeScore()
	if score2 > 0.2 {
		t.Fatalf("new maker should have low score, got %f", score2)
	}
}

func TestMaxBorrow(t *testing.T) {
	cases := []struct {
		score float64
		max   int64
	}{
		{0.1, 0},
		{0.3, 500},
		{0.5, 2000},
		{0.7, 10000},
		{0.95, 50000},
	}
	for _, c := range cases {
		r := &Reputation{Score: c.score}
		if got := r.MaxBorrow(); got != c.max {
			t.Errorf("score %.1f: expected max %d, got %d", c.score, c.max, got)
		}
	}
}

func TestInsufficientBalance(t *testing.T) {
	l := NewLedger()
	l.RegisterMaker("poor-lender")
	l.SetBalance("poor-lender", 100)

	offer := &Offer{
		ID: "offer-1", Lender: "poor-lender", Amount: 1000,
		MinReputation: 0, MaxDurationHrs: 24, InterestPct: 0,
	}
	err := l.Lend(offer)
	if err == nil {
		t.Fatal("should not lend more than balance")
	}
}
