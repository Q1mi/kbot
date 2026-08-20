package simulator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"github.com/Q1mi/kbot-course/insurance/internal/claim"
)

var ErrIdempotencyConflict = errors.New("idempotency key was used for another decision")

type Service struct {
	mu          sync.RWMutex
	policies    map[string]claim.Policy
	decisions   map[string]claim.Decision
	idempotency map[string]idempotencyRecord
}

type idempotencyRecord struct {
	fingerprint [32]byte
	decision    claim.Decision
}

func NewSeeded() *Service {
	return &Service{
		policies: map[string]claim.Policy{
			"POL-1001":    {ID: "POL-1001", Active: true, CoverageLimit: 100000, AutoApproveLimit: 5000},
			"POL-EXPIRED": {ID: "POL-EXPIRED", Active: false, CoverageLimit: 20000, AutoApproveLimit: 1000},
		},
		decisions: make(map[string]claim.Decision), idempotency: make(map[string]idempotencyRecord),
	}
}

func (s *Service) GetPolicy(_ context.Context, policyID string) (claim.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.policies[policyID]
	if !ok {
		return claim.Policy{}, fmt.Errorf("policy %s not found", policyID)
	}
	return policy, nil
}

func (s *Service) Score(_ context.Context, item claim.Claim) (float64, error) {
	if item.ID == "" || item.PolicyID == "" {
		return 0, fmt.Errorf("claim and policy are required")
	}
	risk := 0.1
	if item.Amount >= 50000 {
		risk += 0.45
	}
	if len(item.Documents) == 1 {
		risk += 0.2
	}
	if item.Reason == "identity_theft" {
		risk = 0.95
	}
	return min(risk, 1), nil
}

func (s *Service) SubmitDecision(claimID, idempotencyKey string, decision claim.Decision) (claim.Decision, error) {
	if claimID == "" || idempotencyKey == "" || decision.Action == "" || len(decision.Evidence) == 0 {
		return claim.Decision{}, fmt.Errorf("claim, idempotency key, action and evidence are required")
	}
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%t\x00%t\x00%.6f\x00%v", decision.Action, decision.Eligible, decision.RequiresApproval, decision.RiskScore, decision.Evidence)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.idempotency[idempotencyKey]; ok {
		if existing.fingerprint != fingerprint {
			return claim.Decision{}, ErrIdempotencyConflict
		}
		return existing.decision, nil
	}
	s.decisions[claimID] = decision
	s.idempotency[idempotencyKey] = idempotencyRecord{fingerprint: fingerprint, decision: decision}
	return decision, nil
}
