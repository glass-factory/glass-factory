// Package lending implements the compute token lending/borrowing protocol.
//
// Tokens are compute, not currency. Reputation is the collateral.
// The provenance chain proves work was delivered.
//
// 计算令牌借贷协议 — 借出空闲算力，需要时借入，信誉即抵押。
package lending

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"
)

// Offer is a lending offer from a maker with spare compute.
type Offer struct {
	ID              string  `json:"id"`
	Lender          string  `json:"lender"`           // Ed25519 pubkey hex
	Amount          int64   `json:"amount"`            // tokens available
	MinReputation   float64 `json:"min_reputation"`    // minimum borrower reputation
	MaxDurationHrs  int     `json:"max_duration_hours"`
	InterestPct     float64 `json:"interest_pct"`      // e.g. 5.0 for 5%
	Status          string  `json:"status"`            // available, claimed, expired, cancelled
	ExpiresAt       string  `json:"expires_at"`
	CreatedAt       string  `json:"created_at"`
	Signature       string  `json:"signature"`
}

// Loan is an active borrowing relationship.
type Loan struct {
	ID           string  `json:"id"`
	OfferID      string  `json:"offer_id"`
	Lender       string  `json:"lender"`
	Borrower     string  `json:"borrower"`
	Amount       int64   `json:"amount"`
	RepayAmount  int64   `json:"repay_amount"` // amount + interest
	Status       string  `json:"status"`       // active, settled, defaulted, grace
	Purpose      string  `json:"purpose"`
	RepayBy      string  `json:"repay_by"`
	CreatedAt    string  `json:"created_at"`
	SettledAt    string  `json:"settled_at,omitempty"`
	ProofChain   string  `json:"proof_chain,omitempty"` // proof of work done with borrowed tokens
}

// Reputation tracks a maker's lending trustworthiness.
type Reputation struct {
	Maker            string  `json:"maker"`
	Score            float64 `json:"score"`             // 0.0–1.0
	TotalLoans       int     `json:"total_loans"`
	SuccessfulRepays int     `json:"successful_repays"`
	Defaults         int     `json:"defaults"`
	TotalDelivered   int64   `json:"total_delivered"`   // proven compute tokens delivered
	Penalty          float64 `json:"penalty"`           // accumulated default penalties
	RegisteredAt     string  `json:"registered_at"`
}

// ComputeScore calculates reputation from history.
// Defaults apply a permanent penalty that drags the score down.
func (r *Reputation) ComputeScore() float64 {
	if r.TotalLoans == 0 {
		return 0.0
	}
	repayRate := float64(r.SuccessfulRepays) / float64(r.TotalLoans)
	deliveryFactor := math.Min(1.0, float64(r.TotalDelivered)/10000.0)

	regTime, err := time.Parse(time.RFC3339, r.RegisteredAt)
	ageDays := 0.0
	if err == nil {
		ageDays = time.Since(regTime).Hours() / 24
	}
	ageFactor := math.Min(1.0, ageDays/90.0)

	score := repayRate * deliveryFactor * ageFactor
	score -= r.Penalty
	if score < 0 {
		score = 0
	}
	r.Score = score
	return r.Score
}

// MaxBorrow returns the maximum tokens this maker can borrow.
func (r *Reputation) MaxBorrow() int64 {
	switch {
	case r.Score < 0.3:
		return 0
	case r.Score < 0.5:
		return 500
	case r.Score < 0.7:
		return 2000
	case r.Score < 0.9:
		return 10000
	default:
		return 50000
	}
}

// ── Ledger ──────────────────────────────────────────────────────────────────

// DebtLimits prevents the network from over-leveraging.
type DebtLimits struct {
	MaxDebtToDeliveryRatio float64 // max borrowed / total_delivered (default 0.5)
	MaxActiveLoans         int     // per maker (default 3)
	MaxNetworkDebtPct      float64 // total borrowed / total tokens in network (default 0.3)
	MinBalanceAfterLend    int64   // lender must retain at least this many tokens (default 100)
}

var DefaultDebtLimits = DebtLimits{
	MaxDebtToDeliveryRatio: 0.5,
	MaxActiveLoans:         3,
	MaxNetworkDebtPct:      0.3,
	MinBalanceAfterLend:    100,
}

// Ledger manages offers, loans, and reputation.
type Ledger struct {
	mu          sync.RWMutex
	offers      map[string]*Offer
	loans       map[string]*Loan
	reputations map[string]*Reputation // keyed by maker pubkey
	balances    map[string]int64       // maker pubkey → token balance
	limits      DebtLimits
}

func NewLedger() *Ledger {
	return NewLedgerWithLimits(DefaultDebtLimits)
}

func NewLedgerWithLimits(limits DebtLimits) *Ledger {
	return &Ledger{
		offers:      make(map[string]*Offer),
		loans:       make(map[string]*Loan),
		reputations: make(map[string]*Reputation),
		balances:    make(map[string]int64),
		limits:      limits,
	}
}

// TotalNetworkTokens returns the sum of all balances.
func (l *Ledger) TotalNetworkTokens() int64 {
	var total int64
	for _, b := range l.balances {
		total += b
	}
	return total
}

// TotalOutstandingDebt returns the sum of all active loan amounts.
func (l *Ledger) TotalOutstandingDebt() int64 {
	var total int64
	for _, loan := range l.loans {
		if loan.Status == "active" || loan.Status == "grace" {
			total += loan.Amount
		}
	}
	return total
}

// ActiveLoansFor returns the number of active loans for a maker.
func (l *Ledger) ActiveLoansFor(maker string) int {
	count := 0
	for _, loan := range l.loans {
		if loan.Borrower == maker && (loan.Status == "active" || loan.Status == "grace") {
			count++
		}
	}
	return count
}

// TotalBorrowedBy returns total outstanding debt for a maker.
func (l *Ledger) TotalBorrowedBy(maker string) int64 {
	var total int64
	for _, loan := range l.loans {
		if loan.Borrower == maker && (loan.Status == "active" || loan.Status == "grace") {
			total += loan.Amount
		}
	}
	return total
}

// SetBalance sets a maker's token balance (for testing/init).
func (l *Ledger) SetBalance(maker string, balance int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.balances[maker] = balance
}

// Balance returns a maker's current token balance.
func (l *Ledger) Balance(maker string) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.balances[maker]
}

// RegisterMaker creates a reputation record for a new maker.
func (l *Ledger) RegisterMaker(pubkey string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.reputations[pubkey]; !exists {
		l.reputations[pubkey] = &Reputation{
			Maker:        pubkey,
			RegisteredAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
}

// GetReputation returns a maker's reputation, computing the score.
func (l *Ledger) GetReputation(maker string) (*Reputation, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	r, ok := l.reputations[maker]
	if !ok {
		return nil, fmt.Errorf("maker %s not registered", maker[:16])
	}
	r.ComputeScore()
	return r, nil
}

// Lend creates a lending offer.
func (l *Ledger) Lend(offer *Offer) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	bal := l.balances[offer.Lender]
	if bal < offer.Amount {
		return fmt.Errorf("insufficient balance: have %d, offering %d", bal, offer.Amount)
	}
	if bal-offer.Amount < l.limits.MinBalanceAfterLend {
		return fmt.Errorf("must retain at least %d tokens after lending", l.limits.MinBalanceAfterLend)
	}

	// Same key can't lend and borrow simultaneously
	for _, loan := range l.loans {
		if loan.Borrower == offer.Lender && loan.Status == "active" {
			return fmt.Errorf("cannot lend while you have active loans")
		}
	}

	offer.Status = "available"
	offer.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	l.offers[offer.ID] = offer

	// Reserve tokens from lender's balance
	l.balances[offer.Lender] -= offer.Amount
	return nil
}

// CancelOffer cancels an unclaimed lending offer, returning tokens.
func (l *Ledger) CancelOffer(offerID, lender string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	offer, ok := l.offers[offerID]
	if !ok {
		return fmt.Errorf("offer %s not found", offerID)
	}
	if offer.Lender != lender {
		return fmt.Errorf("not your offer")
	}
	if offer.Status != "available" {
		return fmt.Errorf("offer status %s cannot be cancelled", offer.Status)
	}

	offer.Status = "cancelled"
	l.balances[lender] += offer.Amount
	return nil
}

// Borrow claims a lending offer.
func (l *Ledger) Borrow(loanID, offerID, borrower, purpose string) (*Loan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	offer, ok := l.offers[offerID]
	if !ok {
		return nil, fmt.Errorf("offer %s not found", offerID)
	}
	if offer.Status != "available" {
		return nil, fmt.Errorf("offer not available (status: %s)", offer.Status)
	}
	if offer.Lender == borrower {
		return nil, fmt.Errorf("cannot borrow from yourself")
	}

	// Check reputation
	rep, ok := l.reputations[borrower]
	if !ok {
		return nil, fmt.Errorf("borrower not registered")
	}
	rep.ComputeScore()
	if rep.Score < offer.MinReputation {
		return nil, fmt.Errorf("reputation %.2f below minimum %.2f", rep.Score, offer.MinReputation)
	}
	if offer.Amount > rep.MaxBorrow() {
		return nil, fmt.Errorf("amount %d exceeds max borrow %d for reputation %.2f",
			offer.Amount, rep.MaxBorrow(), rep.Score)
	}

	// Debt limits — prevent over-borrowing
	if l.ActiveLoansFor(borrower) >= l.limits.MaxActiveLoans {
		return nil, fmt.Errorf("max active loans reached (%d)", l.limits.MaxActiveLoans)
	}
	totalBorrowed := l.TotalBorrowedBy(borrower)
	if rep.TotalDelivered > 0 {
		debtRatio := float64(totalBorrowed+offer.Amount) / float64(rep.TotalDelivered)
		if debtRatio > l.limits.MaxDebtToDeliveryRatio {
			return nil, fmt.Errorf("debt-to-delivery ratio %.2f exceeds limit %.2f",
				debtRatio, l.limits.MaxDebtToDeliveryRatio)
		}
	}

	// Network-wide circuit breaker
	networkTotal := l.TotalNetworkTokens()
	if networkTotal > 0 {
		networkDebt := l.TotalOutstandingDebt()
		if float64(networkDebt+offer.Amount)/float64(networkTotal) > l.limits.MaxNetworkDebtPct {
			return nil, fmt.Errorf("network debt limit reached (%.0f%% of total tokens)",
				l.limits.MaxNetworkDebtPct*100)
		}
	}

	// No leveraged lending — can't borrow to re-lend
	for _, o := range l.offers {
		if o.Lender == borrower && o.Status == "available" {
			return nil, fmt.Errorf("cannot borrow while you have active lending offers")
		}
	}

	interest := int64(float64(offer.Amount) * offer.InterestPct / 100.0)
	repayBy := time.Now().Add(time.Duration(offer.MaxDurationHrs) * time.Hour)

	loan := &Loan{
		ID:          loanID,
		OfferID:     offerID,
		Lender:      offer.Lender,
		Borrower:    borrower,
		Amount:      offer.Amount,
		RepayAmount: offer.Amount + interest,
		Status:      "active",
		Purpose:     purpose,
		RepayBy:     repayBy.UTC().Format(time.RFC3339),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	offer.Status = "claimed"
	l.loans[loanID] = loan
	l.balances[borrower] += offer.Amount
	rep.TotalLoans++
	return loan, nil
}

// Repay settles a loan with proof of work.
func (l *Ledger) Repay(loanID, proofChain string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	loan, ok := l.loans[loanID]
	if !ok {
		return fmt.Errorf("loan %s not found", loanID)
	}
	if loan.Status != "active" && loan.Status != "grace" {
		return fmt.Errorf("loan status %s cannot be repaid", loan.Status)
	}

	borrowerBal := l.balances[loan.Borrower]
	if borrowerBal < loan.RepayAmount {
		return fmt.Errorf("insufficient balance to repay: have %d, owe %d", borrowerBal, loan.RepayAmount)
	}

	l.balances[loan.Borrower] -= loan.RepayAmount
	l.balances[loan.Lender] += loan.RepayAmount
	loan.Status = "settled"
	loan.SettledAt = time.Now().UTC().Format(time.RFC3339)
	loan.ProofChain = proofChain

	if rep, ok := l.reputations[loan.Borrower]; ok {
		rep.SuccessfulRepays++
		rep.TotalDelivered += loan.Amount
		rep.ComputeScore()
	}

	return nil
}

// Default marks a loan as defaulted. Called when repay_by + grace period expires.
func (l *Ledger) Default(loanID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	loan, ok := l.loans[loanID]
	if !ok {
		return fmt.Errorf("loan %s not found", loanID)
	}
	if loan.Status != "active" && loan.Status != "grace" {
		return fmt.Errorf("loan status %s cannot be defaulted", loan.Status)
	}

	loan.Status = "defaulted"

	// Reputation penalty — permanent, accumulates
	if rep, ok := l.reputations[loan.Borrower]; ok {
		rep.Defaults++
		rep.Penalty += 0.1 * float64(loan.Amount) / 1000.0
		rep.ComputeScore()
	}

	return nil
}

// ── Signing helpers ─────────────────────────────────────────────────────────

func SignOffer(o *Offer, priv ed25519.PrivateKey, pub ed25519.PublicKey) {
	o.Lender = hex.EncodeToString(pub)
	msg := []byte(fmt.Sprintf("%s|%s|%d|%f|%d",
		o.ID, o.Lender, o.Amount, o.InterestPct, o.MaxDurationHrs))
	sig := ed25519.Sign(priv, msg)
	o.Signature = hex.EncodeToString(sig)
}
