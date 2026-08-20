package claim

import (
	"context"
	"math"
	"testing"
)

type policyReader struct{ policy Policy }

func (r policyReader) GetPolicy(context.Context, string) (Policy, error) { return r.policy, nil }

type fraudScorer float64

func (s fraudScorer) Score(context.Context, Claim) (float64, error) { return float64(s), nil }

func TestTriageRoutesClaimsByEvidenceRiskAndAmount(t *testing.T) {
	policy := Policy{ID: "POL-1", Active: true, CoverageLimit: 100000, AutoApproveLimit: 5000}
	tests := []struct {
		name     string
		claim    Claim
		risk     fraudScorer
		action   string
		approval bool
	}{
		{"small safe claim", Claim{ID: "C-1", PolicyID: "POL-1", Amount: 1200, Documents: []string{"invoice.pdf"}}, 0.1, "auto_approve", false},
		{"large claim", Claim{ID: "C-2", PolicyID: "POL-1", Amount: 12000, Documents: []string{"invoice.pdf"}}, 0.2, "manual_review", true},
		{"high fraud risk", Claim{ID: "C-3", PolicyID: "POL-1", Amount: 800, Documents: []string{"invoice.pdf"}}, 0.9, "fraud_investigation", true},
		{"missing evidence", Claim{ID: "C-4", PolicyID: "POL-1", Amount: 800}, 0.1, "request_documents", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := NewService(policyReader{policy}, test.risk).Triage(context.Background(), test.claim)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action != test.action || decision.RequiresApproval != test.approval || len(decision.Evidence) == 0 {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestTriageRejectsOutOfCoverageClaim(t *testing.T) {
	decision, err := NewService(policyReader{Policy{ID: "POL-1", Active: true, CoverageLimit: 1000}}, fraudScorer(0)).Triage(context.Background(), Claim{ID: "C", PolicyID: "POL-1", Amount: 1001})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Eligible || decision.Action != "reject_over_coverage" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestTriageRejectsNonFiniteMoneyAndRisk(t *testing.T) {
	validPolicy := Policy{ID: "POL-1", Active: true, CoverageLimit: 1000, AutoApproveLimit: 100}
	tests := []struct {
		name   string
		claim  Claim
		policy Policy
		risk   fraudScorer
	}{
		{"NaN amount", Claim{ID: "C-1", PolicyID: "POL-1", Amount: math.NaN(), Documents: []string{"invoice"}}, validPolicy, 0.1},
		{"infinite amount", Claim{ID: "C-1", PolicyID: "POL-1", Amount: math.Inf(1), Documents: []string{"invoice"}}, validPolicy, 0.1},
		{"NaN coverage", Claim{ID: "C-1", PolicyID: "POL-1", Amount: 10, Documents: []string{"invoice"}}, Policy{ID: "POL-1", Active: true, CoverageLimit: math.NaN()}, 0.1},
		{"NaN fraud score", Claim{ID: "C-1", PolicyID: "POL-1", Amount: 10, Documents: []string{"invoice"}}, validPolicy, fraudScorer(math.NaN())},
		{"infinite fraud score", Claim{ID: "C-1", PolicyID: "POL-1", Amount: 10, Documents: []string{"invoice"}}, validPolicy, fraudScorer(math.Inf(1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewService(policyReader{test.policy}, test.risk).Triage(context.Background(), test.claim); err == nil {
				t.Fatal("expected invalid numeric value to be rejected")
			}
		})
	}
}

func TestTriageHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewService(policyReader{}, fraudScorer(0)).Triage(ctx, Claim{})
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}
