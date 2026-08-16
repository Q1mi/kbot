package domain

import "time"

const (
	ClaimAssessing      = "assessing"
	ClaimCollectingDocs = "collecting_documents"
	ClaimSuspectedFraud = "suspected_fraud"
	ClaimApproved       = "approved"
	ClaimRejected       = "rejected"
)

type Coverage struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Limit      float64 `json:"limit"`
	Deductible float64 `json:"deductible"`
	Currency   string  `json:"currency"`
}

type Policy struct {
	ID          string     `json:"id"`
	ProductCode string     `json:"product_code"`
	Version     string     `json:"version"`
	VehicleID   string     `json:"vehicle_id"`
	Status      string     `json:"status"`
	EffectiveAt time.Time  `json:"effective_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	Coverages   []Coverage `json:"coverages"`
}

type UnderwritingCase struct {
	ID                 string   `json:"id"`
	ApplicantID        string   `json:"applicant_id"`
	VehicleID          string   `json:"vehicle_id"`
	ProductCode        string   `json:"product_code"`
	Status             string   `json:"status"`
	PriorClaimCount    int      `json:"prior_claim_count"`
	VehicleAgeYears    int      `json:"vehicle_age_years"`
	RiskFactors        []string `json:"risk_factors"`
	BasePremium        float64  `json:"base_premium"`
	RecommendedPremium float64  `json:"recommended_premium"`
	Currency           string   `json:"currency"`
}

type UnderwritingAssessment struct {
	CaseID             string   `json:"case_id"`
	RiskLevel          string   `json:"risk_level"`
	RecommendedAction  string   `json:"recommended_action"`
	RecommendedPremium float64  `json:"recommended_premium"`
	ReasonCodes        []string `json:"reason_codes"`
	RuleSet            string   `json:"rule_set"`
}

type ClaimDocument struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Checksum string `json:"checksum"`
}

type Claim struct {
	ID              string          `json:"id"`
	PolicyID        string          `json:"policy_id"`
	EventAt         time.Time       `json:"event_at"`
	ReportedAt      time.Time       `json:"reported_at"`
	ClaimedAmount   float64         `json:"claimed_amount"`
	Currency        string          `json:"currency"`
	Status          string          `json:"status"`
	Documents       []ClaimDocument `json:"documents"`
	MissingDocument []string        `json:"missing_documents,omitempty"`
}

type CoverageAssessment struct {
	ClaimID        string   `json:"claim_id"`
	Eligible       bool     `json:"eligible"`
	CoverageCode   string   `json:"coverage_code,omitempty"`
	Deductible     float64  `json:"deductible"`
	MaxPayable     float64  `json:"max_payable"`
	Currency       string   `json:"currency"`
	ReasonCodes    []string `json:"reason_codes"`
	ProductVersion string   `json:"product_version"`
}

type FraudAssessment struct {
	ClaimID   string   `json:"claim_id"`
	RiskScore float64  `json:"risk_score"`
	Signals   []string `json:"signals"`
	RuleSet   string   `json:"rule_set"`
}

type ClaimDecision struct {
	ID             string    `json:"id"`
	ClaimID        string    `json:"claim_id"`
	Decision       string    `json:"decision"`
	ApprovedAmount float64   `json:"approved_amount"`
	Currency       string    `json:"currency"`
	ReasonCodes    []string  `json:"reason_codes"`
	CreatedAt      time.Time `json:"created_at"`
}

type InvestigationCase struct {
	ID        string    `json:"id"`
	ClaimID   string    `json:"claim_id"`
	Signals   []string  `json:"signals"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
