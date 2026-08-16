package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Q1mi/kbot-course/insurance/internal/claim"
	"github.com/Q1mi/kbot-course/insurance/internal/simulator"
)

func New(service *simulator.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /tools/get-policy", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PolicyID string `json:"policy_id"`
		}
		if decodeJSON(w, r, &input) != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		policy, err := service.GetPolicy(r.Context(), strings.TrimSpace(input.PolicyID))
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, policy)
	})
	mux.HandleFunc("POST /tools/score-claim-fraud", func(w http.ResponseWriter, r *http.Request) {
		var item claim.Claim
		if decodeJSON(w, r, &item) != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		score, err := service.Score(r.Context(), item)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]float64{"risk_score": score})
	})
	mux.HandleFunc("POST /tools/submit-claim-decision", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ClaimID  string         `json:"claim_id"`
			Decision claim.Decision `json:"decision"`
		}
		if decodeJSON(w, r, &input) != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		decision, err := service.SubmitDecision(input.ClaimID, r.Header.Get("Idempotency-Key"), input.Decision)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, simulator.ErrIdempotencyConflict) {
				status = http.StatusConflict
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, decision)
	})
	return mux
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
