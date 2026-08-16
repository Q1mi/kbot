package v1

import (
	"errors"
	"math"
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/go-chi/chi/v5"
)

// IAMHandler 用户 / 工作空间列表与工作空间创建。
type IAMHandler struct {
	iamService *iam.Service
}

// NewIAMHandler 创建 IAM 处理器。
func NewIAMHandler(iamService *iam.Service) *IAMHandler {
	return &IAMHandler{iamService: iamService}
}

// ListUsers 列出用户(?limit=&offset=,默认 limit=50)。
// @Summary  列出用户
// @Tags     iam
// @Security BearerAuth
// @Param    limit   query     int  false  "条数(默认 50)"
// @Param    offset  query     int  false  "偏移"
// @Success  200     {array}   map[string]interface{}
// @Router   /users [get]
func (h *IAMHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pageParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	users, err := h.iamService.ListUsers(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// ListWorkspaces 列出工作空间(?limit=&offset=,默认 limit=50)。
// @Summary  列出工作空间
// @Tags     iam
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /workspaces [get]
func (h *IAMHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pageParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wss, err := h.iamService.ListAccessibleWorkspaces(
		r.Context(), middleware.GetUserIDFromContext(r.Context()), limit, offset,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, wss)
}

// CreateWorkspaceRequest 创建工作空间请求体。
type CreateWorkspaceRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ParentID    *string `json:"parent_id,omitempty"`
}

// CreateWorkspace 新建工作空间。
// @Summary  新建工作空间
// @Tags     iam
// @Security BearerAuth
// @Param    body  body      CreateWorkspaceRequest  true  "工作空间"
// @Success  201   {object}  map[string]interface{}
// @Router   /workspaces [post]
func (h *IAMHandler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkspaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	ws, err := h.iamService.CreateWorkspaceForUser(
		r.Context(), middleware.GetUserIDFromContext(r.Context()), req.Name, req.Description, req.ParentID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

type UpsertWorkspaceMemberRequest struct {
	Role string `json:"role"`
}

func (h *IAMHandler) ListWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.iamService.ListWorkspaceMembers(
		r.Context(), middleware.GetUserIDFromContext(r.Context()), chi.URLParam(r, "workspace_id"),
	)
	if err != nil {
		writeIAMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *IAMHandler) UpsertWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	var body UpsertWorkspaceMemberRequest
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	member, err := h.iamService.UpsertWorkspaceMember(
		r.Context(), middleware.GetUserIDFromContext(r.Context()), chi.URLParam(r, "workspace_id"), chi.URLParam(r, "user_id"), body.Role,
	)
	if err != nil {
		writeIAMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (h *IAMHandler) DeleteWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	err := h.iamService.DeleteWorkspaceMember(
		r.Context(), middleware.GetUserIDFromContext(r.Context()), chi.URLParam(r, "workspace_id"), chi.URLParam(r, "user_id"),
	)
	if err != nil {
		writeIAMError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeIAMError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, iam.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, iam.ErrLastOwner):
		http.Error(w, err.Error(), http.StatusConflict)
	case len(err.Error()) >= 7 && err.Error()[:7] == "invalid":
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func pageParams(r *http.Request) (int32, int32, error) {
	limit, err := queryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return 0, 0, err
	}
	offset, err := queryInt(r, "offset", 0, 0, math.MaxInt32)
	if err != nil {
		return 0, 0, err
	}
	return int32(limit), int32(offset), nil
}
