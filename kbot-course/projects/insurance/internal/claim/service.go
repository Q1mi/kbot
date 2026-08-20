// Package claim 是保险理赔 Agent 的垂直领域核心。
package claim

import (
	"context"
	"fmt"
	"math"
	"strings"
)

type Claim struct {
	ID        string   `json:"id"`
	PolicyID  string   `json:"policy_id"`
	Reason    string   `json:"reason"`
	Amount    float64  `json:"amount"`
	Documents []string `json:"documents"`
}
type Decision struct {
	Eligible         bool     `json:"eligible"`
	RequiresApproval bool     `json:"requires_approval"`
	RiskScore        float64  `json:"risk_score"`
	Action           string   `json:"action"`
	Evidence         []string `json:"evidence"`
}
type Policy struct {
	ID               string  `json:"id"`
	Active           bool    `json:"active"`
	CoverageLimit    float64 `json:"coverage_limit"`
	AutoApproveLimit float64 `json:"auto_approve_limit"`
}
type PolicyReader interface {
	GetPolicy(context.Context, string) (Policy, error)
}
type FraudScorer interface {
	Score(context.Context, Claim) (float64, error)
}
type Service struct {
	policies PolicyReader
	fraud    FraudScorer
}

func NewService(policies PolicyReader, fraud FraudScorer) *Service {
	return &Service{policies: policies, fraud: fraud}
}

// Triage 将确定性业务规则与模型/工具能力分开，所有结论都携带可解释证据。
func (s *Service) Triage(ctx context.Context, claim Claim) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if s.policies == nil || s.fraud == nil {
		return Decision{}, fmt.Errorf("policy reader and fraud scorer are required")
	}
	if strings.TrimSpace(claim.ID) == "" || strings.TrimSpace(claim.PolicyID) == "" || !finitePositive(claim.Amount) {
		return Decision{}, fmt.Errorf("claim id, policy id and positive amount are required")
	}
	policy, err := s.policies.GetPolicy(ctx, claim.PolicyID)
	if err != nil {
		return Decision{}, fmt.Errorf("get policy: %w", err)
	}
	if policy.ID != claim.PolicyID {
		return Decision{}, fmt.Errorf("policy reader returned %q for claim policy %q", policy.ID, claim.PolicyID)
	}
	if !finiteNonNegative(policy.CoverageLimit) || !finiteNonNegative(policy.AutoApproveLimit) || policy.AutoApproveLimit > policy.CoverageLimit {
		return Decision{}, fmt.Errorf("policy limits must be finite, non-negative and ordered")
	}
	decision := Decision{Evidence: []string{fmt.Sprintf("policy:%s", policy.ID)}}
	if !policy.Active {
		decision.Action = "reject_inactive_policy"
		decision.Evidence = append(decision.Evidence, "policy_inactive")
		return decision, nil
	}
	if claim.Amount > policy.CoverageLimit {
		decision.Action = "reject_over_coverage"
		decision.Evidence = append(decision.Evidence, "amount_exceeds_coverage")
		return decision, nil
	}
	decision.Eligible = true
	if len(claim.Documents) == 0 {
		decision.Action = "request_documents"
		decision.Evidence = append(decision.Evidence, "documents_missing")
		return decision, nil
	}
	risk, err := s.fraud.Score(ctx, claim)
	if err != nil {
		return Decision{}, fmt.Errorf("score fraud risk: %w", err)
	}
	if math.IsNaN(risk) || math.IsInf(risk, 0) || risk < 0 || risk > 1 {
		return Decision{}, fmt.Errorf("fraud score must be within [0,1]")
	}
	decision.RiskScore = risk
	decision.Evidence = append(decision.Evidence, fmt.Sprintf("fraud_score:%.2f", risk))
	switch {
	case risk >= 0.8:
		decision.Action, decision.RequiresApproval = "fraud_investigation", true
	case claim.Amount > policy.AutoApproveLimit:
		decision.Action, decision.RequiresApproval = "manual_review", true
	default:
		decision.Action = "auto_approve"
	}
	return decision, nil
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
