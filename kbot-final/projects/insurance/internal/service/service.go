package service

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Q1mi/kbot-insurance/internal/domain"
)

var (
	ErrNotFound          = errors.New("insurance resource not found")
	ErrInvalidTransition = errors.New("invalid claim state transition")
	ErrDecisionGuard     = errors.New("claim decision rejected by deterministic guard")
	ErrIdempotencyKey    = errors.New("idempotency_key is required")
)

const fraudHumanReviewThreshold = 0.70

type Service struct {
	mu             sync.RWMutex
	policies       map[string]domain.Policy
	underwriting   map[string]domain.UnderwritingCase
	claims         map[string]domain.Claim
	fraud          map[string]domain.FraudAssessment
	decisions      map[string]domain.ClaimDecision
	investigations map[string]domain.InvestigationCase
	idempotency    map[string]any
}

func NewSeeded() *Service {
	effective := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	policy := domain.Policy{
		ID: "POL-CAR-2026-0001", ProductCode: "AUTO-COLLISION", Version: "2026.07",
		VehicleID: "VEH-DEMO-001", Status: "active", EffectiveAt: effective,
		ExpiresAt: effective.AddDate(1, 0, 0),
		Coverages: []domain.Coverage{{Code: "COLLISION", Name: "车辆损失险", Limit: 20000, Deductible: 500, Currency: "CNY"}},
	}
	return &Service{
		policies: map[string]domain.Policy{policy.ID: policy},
		underwriting: map[string]domain.UnderwritingCase{
			"UW-2026-0001": {
				ID: "UW-2026-0001", ApplicantID: "CUS-DEMO-002", VehicleID: "VEH-DEMO-002",
				ProductCode: "AUTO-COLLISION", Status: "referred", PriorClaimCount: 3,
				VehicleAgeYears: 8, RiskFactors: []string{"HIGH_PRIOR_CLAIM_FREQUENCY", "OLDER_VEHICLE"},
				BasePremium: 3200, RecommendedPremium: 4160, Currency: "CNY",
			},
		},
		claims: map[string]domain.Claim{
			"CLM-2026-0001": {ID: "CLM-2026-0001", PolicyID: policy.ID, EventAt: effective.Add(14 * 24 * time.Hour), ReportedAt: effective.Add(15 * 24 * time.Hour), ClaimedAmount: 6800, Currency: "CNY", Status: domain.ClaimAssessing, Documents: completeDocuments("clean")},
			"CLM-2026-0002": {ID: "CLM-2026-0002", PolicyID: policy.ID, EventAt: effective.Add(-50 * time.Minute), ReportedAt: effective.Add(2 * time.Hour), ClaimedAmount: 12800, Currency: "CNY", Status: domain.ClaimAssessing, Documents: completeDocuments("duplicate")},
		},
		fraud: map[string]domain.FraudAssessment{
			"CLM-2026-0001": {ClaimID: "CLM-2026-0001", RiskScore: 0.08, Signals: nil, RuleSet: "auto-fraud-2026.08"},
			"CLM-2026-0002": {ClaimID: "CLM-2026-0002", RiskScore: 0.92, Signals: []string{"EVENT_BEFORE_POLICY_EFFECTIVE", "DUPLICATE_DAMAGE_IMAGE"}, RuleSet: "auto-fraud-2026.08"},
		},
		decisions: make(map[string]domain.ClaimDecision), investigations: make(map[string]domain.InvestigationCase), idempotency: make(map[string]any),
	}
}

func (s *Service) GetUnderwritingCase(id string) (domain.UnderwritingCase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.underwriting[id]
	if !ok {
		return domain.UnderwritingCase{}, ErrNotFound
	}
	return item, nil
}

func (s *Service) AssessUnderwriting(id string) (domain.UnderwritingAssessment, error) {
	item, err := s.GetUnderwritingCase(id)
	if err != nil {
		return domain.UnderwritingAssessment{}, err
	}
	assessment := domain.UnderwritingAssessment{
		CaseID: item.ID, RiskLevel: "standard", RecommendedAction: "approve",
		RecommendedPremium: item.BasePremium, RuleSet: "auto-underwriting-2026.08",
	}
	if item.PriorClaimCount >= 3 || item.VehicleAgeYears >= 8 {
		assessment.RiskLevel = "high"
		assessment.RecommendedAction = "refer_manual_underwriting"
		assessment.RecommendedPremium = item.RecommendedPremium
		assessment.ReasonCodes = append([]string(nil), item.RiskFactors...)
	}
	return assessment, nil
}

type UnderwritingDecisionRequest struct {
	CaseID, Decision, IdempotencyKey string
	Premium                          float64
	ReasonCodes                      []string
	DryRun                           bool
}

func (s *Service) ApproveUnderwriting(req UnderwritingDecisionRequest) (domain.UnderwritingCase, error) {
	if req.IdempotencyKey == "" {
		return domain.UnderwritingCase{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["underwriting:"+req.IdempotencyKey]; ok {
		return cached.(domain.UnderwritingCase), nil
	}
	item, ok := s.underwriting[req.CaseID]
	if !ok {
		return domain.UnderwritingCase{}, ErrNotFound
	}
	if item.Status != "referred" || req.Decision != "approve" || req.Premium < item.BasePremium {
		return domain.UnderwritingCase{}, ErrDecisionGuard
	}
	item.Status = "approved"
	item.RecommendedPremium = req.Premium
	if req.DryRun {
		return item, nil
	}
	s.underwriting[item.ID] = item
	s.idempotency["underwriting:"+req.IdempotencyKey] = item
	return item, nil
}

func completeDocuments(checksum string) []domain.ClaimDocument {
	return []domain.ClaimDocument{{Type: "accident_report", Status: "verified", Checksum: checksum + "-report"}, {Type: "damage_photo", Status: "verified", Checksum: checksum + "-image"}, {Type: "repair_invoice", Status: "verified", Checksum: checksum + "-invoice"}}
}

func (s *Service) GetPolicy(id string) (domain.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.policies[id]
	if !ok {
		return domain.Policy{}, ErrNotFound
	}
	return item, nil
}

func (s *Service) GetClaim(id string) (domain.Claim, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.claims[id]
	if !ok {
		return domain.Claim{}, ErrNotFound
	}
	return item, nil
}

func (s *Service) EvaluateCoverage(claimID string) (domain.CoverageAssessment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	claim, ok := s.claims[claimID]
	if !ok {
		return domain.CoverageAssessment{}, ErrNotFound
	}
	policy, ok := s.policies[claim.PolicyID]
	if !ok {
		return domain.CoverageAssessment{}, ErrNotFound
	}
	result := domain.CoverageAssessment{ClaimID: claim.ID, Currency: claim.Currency, ProductVersion: policy.Version}
	if policy.Status != "active" {
		result.ReasonCodes = append(result.ReasonCodes, "POLICY_INACTIVE")
		return result, nil
	}
	if claim.EventAt.Before(policy.EffectiveAt) || claim.EventAt.After(policy.ExpiresAt) {
		result.ReasonCodes = append(result.ReasonCodes, "EVENT_OUTSIDE_COVERAGE_PERIOD")
		return result, nil
	}
	coverage := policy.Coverages[0]
	result.Eligible, result.CoverageCode, result.Deductible = true, coverage.Code, coverage.Deductible
	result.MaxPayable = math.Max(0, math.Min(claim.ClaimedAmount, coverage.Limit)-coverage.Deductible)
	result.ReasonCodes = []string{"COVERED_COLLISION", "DEDUCTIBLE_APPLIED"}
	return result, nil
}

func (s *Service) FraudFeatures(claimID string) (domain.FraudAssessment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.fraud[claimID]
	if !ok {
		return domain.FraudAssessment{}, ErrNotFound
	}
	return item, nil
}

type RequestDocumentsRequest struct {
	ClaimID        string
	Documents      []string
	IdempotencyKey string
	DryRun         bool
}

func (s *Service) RequestDocuments(req RequestDocumentsRequest) (domain.Claim, error) {
	if req.IdempotencyKey == "" {
		return domain.Claim{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["docs:"+req.IdempotencyKey]; ok {
		return cached.(domain.Claim), nil
	}
	claim, ok := s.claims[req.ClaimID]
	if !ok {
		return domain.Claim{}, ErrNotFound
	}
	if claim.Status != domain.ClaimAssessing || len(req.Documents) == 0 {
		return domain.Claim{}, ErrInvalidTransition
	}
	claim.Status, claim.MissingDocument = domain.ClaimCollectingDocs, append([]string(nil), req.Documents...)
	if req.DryRun {
		return claim, nil
	}
	s.claims[claim.ID] = claim
	s.idempotency["docs:"+req.IdempotencyKey] = claim
	return claim, nil
}

type ClaimActionRequest struct {
	ClaimID, Reason, IdempotencyKey string
	ApprovedAmount                  float64
	ReasonCodes                     []string
	DryRun                          bool
}

func (s *Service) HoldClaim(req ClaimActionRequest) (domain.Claim, error) {
	if req.IdempotencyKey == "" {
		return domain.Claim{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["hold:"+req.IdempotencyKey]; ok {
		return cached.(domain.Claim), nil
	}
	claim, ok := s.claims[req.ClaimID]
	if !ok {
		return domain.Claim{}, ErrNotFound
	}
	risk := s.fraud[claim.ID]
	if claim.Status != domain.ClaimAssessing || risk.RiskScore < fraudHumanReviewThreshold {
		return domain.Claim{}, ErrDecisionGuard
	}
	claim.Status = domain.ClaimSuspectedFraud
	if req.DryRun {
		return claim, nil
	}
	s.claims[claim.ID] = claim
	s.idempotency["hold:"+req.IdempotencyKey] = claim
	return claim, nil
}

func (s *Service) ApproveClaim(req ClaimActionRequest) (domain.ClaimDecision, error) {
	if req.IdempotencyKey == "" {
		return domain.ClaimDecision{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["approve:"+req.IdempotencyKey]; ok {
		return cached.(domain.ClaimDecision), nil
	}
	claim, ok := s.claims[req.ClaimID]
	if !ok {
		return domain.ClaimDecision{}, ErrNotFound
	}
	if claim.Status != domain.ClaimAssessing {
		return domain.ClaimDecision{}, ErrInvalidTransition
	}
	risk := s.fraud[claim.ID]
	if risk.RiskScore >= fraudHumanReviewThreshold {
		return domain.ClaimDecision{}, ErrDecisionGuard
	}
	policy := s.policies[claim.PolicyID]
	if claim.EventAt.Before(policy.EffectiveAt) || len(policy.Coverages) == 0 {
		return domain.ClaimDecision{}, ErrDecisionGuard
	}
	coverage := policy.Coverages[0]
	maxPayable := math.Max(0, math.Min(claim.ClaimedAmount, coverage.Limit)-coverage.Deductible)
	if req.ApprovedAmount <= 0 || req.ApprovedAmount > maxPayable {
		return domain.ClaimDecision{}, ErrDecisionGuard
	}
	claim.Status = domain.ClaimApproved
	decisionID := fmt.Sprintf("DEC-%04d", len(s.decisions)+1)
	if req.DryRun {
		decisionID = "DEC-DRY-RUN"
	}
	decision := domain.ClaimDecision{ID: decisionID, ClaimID: claim.ID, Decision: "approved", ApprovedAmount: req.ApprovedAmount, Currency: claim.Currency, ReasonCodes: append([]string(nil), req.ReasonCodes...), CreatedAt: time.Now().UTC()}
	if req.DryRun {
		return decision, nil
	}
	s.claims[claim.ID] = claim
	s.decisions[decision.ID] = decision
	s.idempotency["approve:"+req.IdempotencyKey] = decision
	return decision, nil
}

func (s *Service) OpenInvestigation(req ClaimActionRequest) (domain.InvestigationCase, error) {
	if req.IdempotencyKey == "" {
		return domain.InvestigationCase{}, ErrIdempotencyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, ok := s.idempotency["investigation:"+req.IdempotencyKey]; ok {
		return cached.(domain.InvestigationCase), nil
	}
	claim, ok := s.claims[req.ClaimID]
	if !ok {
		return domain.InvestigationCase{}, ErrNotFound
	}
	if claim.Status != domain.ClaimSuspectedFraud && !(req.DryRun && claim.Status == domain.ClaimAssessing && s.fraud[claim.ID].RiskScore >= fraudHumanReviewThreshold) {
		return domain.InvestigationCase{}, ErrInvalidTransition
	}
	risk := s.fraud[claim.ID]
	itemID := fmt.Sprintf("INV-%04d", len(s.investigations)+1)
	if req.DryRun {
		itemID = "INV-DRY-RUN"
	}
	item := domain.InvestigationCase{ID: itemID, ClaimID: claim.ID, Signals: append([]string(nil), risk.Signals...), Status: "open", CreatedAt: time.Now().UTC()}
	if req.DryRun {
		return item, nil
	}
	s.investigations[item.ID] = item
	s.idempotency["investigation:"+req.IdempotencyKey] = item
	return item, nil
}
