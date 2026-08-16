package service

import (
	"errors"
	"testing"

	"github.com/Q1mi/kbot-insurance/internal/domain"
)

func TestCleanClaimCanBeApprovedWithinDeterministicLimit(t *testing.T) {
	svc := NewSeeded()
	coverage, err := svc.EvaluateCoverage("CLM-2026-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !coverage.Eligible || coverage.MaxPayable != 6300 {
		t.Fatalf("unexpected coverage: %#v", coverage)
	}
	decision, err := svc.ApproveClaim(ClaimActionRequest{ClaimID: "CLM-2026-0001", ApprovedAmount: 6300, ReasonCodes: coverage.ReasonCodes, IdempotencyKey: "approve-clean-1"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "approved" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	claim, _ := svc.GetClaim("CLM-2026-0001")
	if claim.Status != domain.ClaimApproved {
		t.Fatalf("unexpected status %q", claim.Status)
	}
}

func TestHighRiskClaimRequiresHoldAndInvestigation(t *testing.T) {
	svc := NewSeeded()
	_, err := svc.ApproveClaim(ClaimActionRequest{ClaimID: "CLM-2026-0002", ApprovedAmount: 1000, IdempotencyKey: "unsafe-approve"})
	if !errors.Is(err, ErrDecisionGuard) {
		t.Fatalf("want decision guard, got %v", err)
	}
	claim, err := svc.HoldClaim(ClaimActionRequest{ClaimID: "CLM-2026-0002", Reason: "high fraud score", IdempotencyKey: "hold-1"})
	if err != nil || claim.Status != domain.ClaimSuspectedFraud {
		t.Fatalf("claim=%#v err=%v", claim, err)
	}
	item, err := svc.OpenInvestigation(ClaimActionRequest{ClaimID: claim.ID, IdempotencyKey: "investigation-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Signals) != 2 {
		t.Fatalf("unexpected signals: %#v", item.Signals)
	}
}

func TestApprovalIsIdempotent(t *testing.T) {
	svc := NewSeeded()
	req := ClaimActionRequest{ClaimID: "CLM-2026-0001", ApprovedAmount: 6000, ReasonCodes: []string{"COVERED_COLLISION"}, IdempotencyKey: "approve-replay"}
	first, err := svc.ApproveClaim(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ApproveClaim(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent replay created %q and %q", first.ID, second.ID)
	}
}

func TestHighRiskUnderwritingCaseRequiresManualDecision(t *testing.T) {
	svc := NewSeeded()
	assessment, err := svc.AssessUnderwriting("UW-2026-0001")
	if err != nil {
		t.Fatal(err)
	}
	if assessment.RecommendedAction != "refer_manual_underwriting" || assessment.RecommendedPremium != 4160 {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
	_, err = svc.ApproveUnderwriting(UnderwritingDecisionRequest{CaseID: "UW-2026-0001", Decision: "approve", Premium: 3000, IdempotencyKey: "uw-invalid"})
	if !errors.Is(err, ErrDecisionGuard) {
		t.Fatalf("want decision guard, got %v", err)
	}
	approved, err := svc.ApproveUnderwriting(UnderwritingDecisionRequest{CaseID: "UW-2026-0001", Decision: "approve", Premium: 4160, ReasonCodes: assessment.ReasonCodes, IdempotencyKey: "uw-approve"})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != "approved" {
		t.Fatalf("unexpected case: %#v", approved)
	}
}

func TestDryRunValidatesSensitiveActionsWithoutChangingState(t *testing.T) {
	svc := NewSeeded()
	preview, err := svc.HoldClaim(ClaimActionRequest{ClaimID: "CLM-2026-0002", Reason: "tool preflight", IdempotencyKey: "dry-hold", DryRun: true})
	if err != nil || preview.Status != domain.ClaimSuspectedFraud {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
	claim, err := svc.GetClaim("CLM-2026-0002")
	if err != nil || claim.Status != domain.ClaimAssessing {
		t.Fatalf("dry run changed persisted claim: %#v err=%v", claim, err)
	}
	decision, err := svc.ApproveClaim(ClaimActionRequest{ClaimID: "CLM-2026-0001", ApprovedAmount: 6300, ReasonCodes: []string{"COVERED_COLLISION"}, IdempotencyKey: "dry-approve", DryRun: true})
	if err != nil || decision.ID != "DEC-DRY-RUN" {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
	claim, _ = svc.GetClaim("CLM-2026-0001")
	if claim.Status != domain.ClaimAssessing {
		t.Fatalf("dry run changed clean claim status to %q", claim.Status)
	}
}
