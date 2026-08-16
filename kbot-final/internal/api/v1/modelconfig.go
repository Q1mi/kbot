package v1

import (
	"errors"
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/go-chi/chi/v5"
)

type ModelConfigHandler struct{ svc *modelconfig.Service }

func NewModelConfigHandler(svc *modelconfig.Service) *ModelConfigHandler {
	return &ModelConfigHandler{svc: svc}
}

func (h *ModelConfigHandler) CreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	var req modelconfig.CreateProviderAccountRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(r.Context())
	req.CreatedBy = middleware.GetUserIDFromContext(r.Context())
	out, err := h.svc.CreateProviderAccount(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *ModelConfigHandler) ListProviderAccounts(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListProviderAccounts(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ModelConfigHandler) RotateProviderAPIKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	err := h.svc.RotateProviderAPIKey(
		r.Context(),
		middleware.GetWorkspaceIDFromContext(r.Context()),
		chi.URLParam(r, "account_id"),
		body.APIKey,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ModelConfigHandler) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	var req modelconfig.CreateDeploymentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(r.Context())
	req.CreatedBy = middleware.GetUserIDFromContext(r.Context())
	out, err := h.svc.CreateDeployment(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *ModelConfigHandler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListDeployments(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ModelConfigHandler) UpdateDeploymentPricing(w http.ResponseWriter, r *http.Request) {
	var req modelconfig.UpdateDeploymentPricingRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(r.Context())
	out, err := h.svc.UpdateDeploymentPricing(r.Context(), chi.URLParam(r, "deployment_id"), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ModelConfigHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var req modelconfig.CreateProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspaceID = middleware.GetWorkspaceIDFromContext(r.Context())
	req.CreatedBy = middleware.GetUserIDFromContext(r.Context())
	p, v, err := h.svc.CreateProfile(r.Context(), req)
	if err != nil {
		if errors.Is(err, modelconfig.ErrProfileNameExists) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "同一工作空间内已存在同名 Profile，请更换名称",
			})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"profile": p, "version": v})
}

func (h *ModelConfigHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListProfiles(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ModelConfigHandler) CreateProfileVersion(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.EnsureProfileWorkspace(
		r.Context(), chi.URLParam(r, "profile_id"), middleware.GetWorkspaceIDFromContext(r.Context()),
	); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var body struct {
		PrimaryDeploymentID   string   `json:"primary_deployment_id"`
		FallbackDeploymentIDs []string `json:"fallback_deployment_ids"`
		ClassificationMax     string   `json:"classification_max"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	out, err := h.svc.CreateProfileVersion(
		r.Context(), chi.URLParam(r, "profile_id"), body.PrimaryDeploymentID,
		body.FallbackDeploymentIDs, body.ClassificationMax,
		middleware.GetUserIDFromContext(r.Context()),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *ModelConfigHandler) ListProfileVersions(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.EnsureProfileWorkspace(
		r.Context(), chi.URLParam(r, "profile_id"), middleware.GetWorkspaceIDFromContext(r.Context()),
	); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, err := h.svc.ListProfileVersions(r.Context(), chi.URLParam(r, "profile_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ModelConfigHandler) ListWorkspaceProfileVersions(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListWorkspaceProfileVersions(r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *ModelConfigHandler) BindProject(w http.ResponseWriter, r *http.Request) {
	var body modelconfig.ProjectBinding
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.WorkspaceID = middleware.GetWorkspaceIDFromContext(r.Context())
	if err := h.svc.BindProject(r.Context(), &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *ModelConfigHandler) GetProjectBinding(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetProjectBinding(
		r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()), r.URL.Query().Get("env"),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
