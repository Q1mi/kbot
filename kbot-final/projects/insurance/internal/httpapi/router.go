package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Q1mi/kbot-insurance/internal/agentmock"
	"github.com/Q1mi/kbot-insurance/internal/service"
)

func New(svc *service.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "project": "insurance"})
	})
	mux.HandleFunc("POST /v1/chat/completions", agentmock.Handler)
	mux.HandleFunc("POST /tools/get_policy", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PolicyID string `json:"policy_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.GetPolicy(in.PolicyID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/get_underwriting_case", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CaseID string `json:"case_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.GetUnderwritingCase(in.CaseID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/assess_underwriting", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CaseID string `json:"case_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.AssessUnderwriting(in.CaseID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/approve_underwriting", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CaseID         string   `json:"case_id"`
			Decision       string   `json:"decision"`
			Premium        float64  `json:"premium"`
			ReasonCodes    []string `json:"reason_codes"`
			IdempotencyKey string   `json:"idempotency_key"`
			DryRun         bool     `json:"dry_run"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.ApproveUnderwriting(service.UnderwritingDecisionRequest{CaseID: in.CaseID, Decision: in.Decision, Premium: in.Premium, ReasonCodes: in.ReasonCodes, IdempotencyKey: in.IdempotencyKey, DryRun: in.DryRun})
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/get_claim", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClaimID string `json:"claim_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.GetClaim(in.ClaimID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/evaluate_coverage", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClaimID string `json:"claim_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.EvaluateCoverage(in.ClaimID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/get_fraud_features", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClaimID string `json:"claim_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.FraudFeatures(in.ClaimID)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/request_additional_documents", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ClaimID   string   `json:"claim_id"`
			Documents []string `json:"documents"`
			Key       string   `json:"idempotency_key"`
			DryRun    bool     `json:"dry_run"`
		}
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.RequestDocuments(service.RequestDocumentsRequest{ClaimID: in.ClaimID, Documents: in.Documents, IdempotencyKey: in.Key, DryRun: in.DryRun})
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/hold_claim_payment", func(w http.ResponseWriter, r *http.Request) {
		var in actionInput
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.HoldClaim(in.request())
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/approve_claim", func(w http.ResponseWriter, r *http.Request) {
		var in actionInput
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.ApproveClaim(in.request())
		respond(w, out, err)
	})
	mux.HandleFunc("POST /tools/open_fraud_investigation", func(w http.ResponseWriter, r *http.Request) {
		var in actionInput
		if !decode(w, r, &in) {
			return
		}
		out, err := svc.OpenInvestigation(in.request())
		respond(w, out, err)
	})
	return mux
}

type actionInput struct {
	ClaimID        string   `json:"claim_id"`
	Reason         string   `json:"reason"`
	ApprovedAmount float64  `json:"approved_amount"`
	ReasonCodes    []string `json:"reason_codes"`
	Key            string   `json:"idempotency_key"`
	DryRun         bool     `json:"dry_run"`
}

func (in actionInput) request() service.ClaimActionRequest {
	return service.ClaimActionRequest{ClaimID: in.ClaimID, Reason: in.Reason, ApprovedAmount: in.ApprovedAmount, ReasonCodes: in.ReasonCodes, IdempotencyKey: in.Key, DryRun: in.DryRun}
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	return true
}
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	status := http.StatusUnprocessableEntity
	if errors.Is(err, service.ErrNotFound) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
