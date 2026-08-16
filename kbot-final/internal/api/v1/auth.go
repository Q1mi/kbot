// Package v1 提供API v1接口实现
package v1

import (
	"errors"
	"net/http"

	"github.com/Q1mi/kbot/internal/platform/iam"
)

// AuthHandler 认证处理器
type AuthHandler struct {
	iamService *iam.Service
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(iamService *iam.Service) *AuthHandler {
	return &AuthHandler{iamService: iamService}
}

// Login 用户登录
// @Summary  用户登录
// @Tags     auth
// @Accept   json
// @Produce  json
// @Param    body  body      iam.LoginRequest   true  "邮箱 + 密码"
// @Success  200   {object}  iam.LoginResponse
// @Failure  401   {string}  string  "认证失败"
// @Router   /auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req iam.LoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.iamService.Login(r.Context(), req)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// CreateUserRequest 创建用户请求
type CreateUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// CreateUser 创建用户
// @Summary  注册用户
// @Tags     auth
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    body  body      CreateUserRequest  true  "用户信息"
// @Success  201   {object}  map[string]interface{}
// @Router   /auth/register [post]
func (h *AuthHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := h.iamService.CreateUser(r.Context(), req.Email, req.Password, req.Name)
	if err != nil {
		if errors.Is(err, iam.ErrInvalidEmail) || errors.Is(err, iam.ErrWeakPassword) || errors.Is(err, iam.ErrNameRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, user)
}
