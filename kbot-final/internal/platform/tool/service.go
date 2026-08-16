// Package tool 提供Tool Registry服务
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/util"
)

// Service Tool Registry服务
type Service struct {
	store  Store
	cipher CredentialCipher
	policy *EndpointPolicy
}

type CredentialCipher interface {
	Encrypt(plain string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

// Store Tool存储接口
type Store interface {
	// Tool相关
	GetTool(ctx context.Context, toolID string) (*domain.Tool, error)
	CreateTool(ctx context.Context, tool *domain.Tool) error
	ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error)

	// ToolVersion相关
	GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error)
	CreateToolVersion(ctx context.Context, version *domain.ToolVersion) error
	GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error)
	GetToolLatestPublishedVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error)
	ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error)
	UpdateToolVersionStatus(ctx context.Context, versionID, status string) error
	ListLegacyToolAuthVersions(ctx context.Context) ([]*domain.ToolVersion, error)
	EncryptToolVersionAuth(ctx context.Context, versionID string, ciphertext []byte) error
	CreateInvocation(ctx context.Context, invocation *domain.ToolInvocation) error
	CompleteInvocation(ctx context.Context, invocationID, result, status string, latencyMS int, errorMessage string) error
	CreateSandboxExecution(ctx context.Context, execution *domain.SandboxExecution) error

	// ToolTestRun相关
	CreateTestRun(ctx context.Context, testRun *domain.ToolTestRun) error
	GetToolLastSuccessfulTestRun(ctx context.Context, toolID string) (*domain.ToolTestRun, error)
	GetToolLastSuccessfulTestRunForVersion(ctx context.Context, versionID string) (*domain.ToolTestRun, error)
}

type toolBundleStore interface {
	CreateToolWithVersion(context.Context, *domain.Tool, *domain.ToolVersion) error
}

// CreateToolVersionRequest 创建新的不可变 Tool 配置版本。
type CreateToolVersionRequest struct {
	SchemaJSON     string `json:"schema_json"`
	EndpointConfig string `json:"endpoint_config"`
	AuthConfig     string `json:"auth_config"`
	RetryPolicy    string `json:"retry_policy"`
	CreatedBy      string `json:"created_by"`
}

// CreateToolVersion 复制并覆盖当前配置，创建一个新的 draft 版本。
func (s *Service) CreateToolVersion(ctx context.Context, toolID string, req CreateToolVersionRequest) (*domain.ToolVersion, error) {
	tool, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return nil, err
	}
	current, err := s.store.GetToolCurrentVersion(ctx, tool.ID)
	if err != nil {
		return nil, err
	}
	if req.SchemaJSON == "" {
		req.SchemaJSON = current.SchemaJSON
	}
	if req.EndpointConfig == "" {
		req.EndpointConfig = current.EndpointConfig
	}
	if req.AuthConfig == "" {
		req.AuthConfig, err = s.authPlain(current)
		if err != nil {
			return nil, err
		}
	}
	if req.RetryPolicy == "" {
		req.RetryPolicy = current.RetryPolicy
	}
	cfg, err := toolConfigFromStrings(tool.SourceType, req.SchemaJSON, req.EndpointConfig, req.AuthConfig)
	if err != nil {
		return nil, err
	}
	if err := s.validateEndpoint(ctx, cfg); err != nil {
		return nil, err
	}
	versions, err := s.store.ListToolVersions(ctx, toolID)
	if err != nil {
		return nil, err
	}
	next := 1
	if len(versions) > 0 {
		next = versions[0].Version + 1
	}
	v := &domain.ToolVersion{
		ID: util.GenerateID(), ToolID: toolID, Version: next,
		SchemaJSON: req.SchemaJSON, EndpointConfig: req.EndpointConfig,
		AuthConfig: req.AuthConfig, RetryPolicy: req.RetryPolicy,
		Status: "draft", CreatedBy: req.CreatedBy, CreatedAt: time.Now(),
	}
	if err := s.prepareVersionAuth(v); err != nil {
		return nil, err
	}
	if err := s.store.CreateToolVersion(ctx, v); err != nil {
		return nil, fmt.Errorf("create tool version: %w", err)
	}
	return v, nil
}

// NewService 创建Tool Registry服务
func NewService(store Store) *Service {
	return &Service{store: store}
}

// ConfigureSecurity 开启 Tool 凭证加密和出网目标控制。
func (s *Service) ConfigureSecurity(cipher CredentialCipher, policy *EndpointPolicy) {
	s.cipher = cipher
	s.policy = policy
}

func (s *Service) ConfigureEndpointPolicy(policy *EndpointPolicy) { s.policy = policy }

// MigrateLegacyCredentials 把历史明文 auth_config 原地转换为密文。
func (s *Service) MigrateLegacyCredentials(ctx context.Context) error {
	if s.cipher == nil {
		return nil
	}
	versions, err := s.store.ListLegacyToolAuthVersions(ctx)
	if err != nil {
		return err
	}
	for _, version := range versions {
		ciphertext, err := s.cipher.Encrypt(version.AuthConfig)
		if err != nil {
			return fmt.Errorf("encrypt tool version %s auth: %w", version.ID, err)
		}
		if err := s.store.EncryptToolVersionAuth(ctx, version.ID, ciphertext); err != nil {
			return fmt.Errorf("persist tool version %s auth: %w", version.ID, err)
		}
	}
	return nil
}

// BeginToolInvocation 在外部副作用发生前持久化 running 记录；同一会话的 tool_call_id 只允许一次。
func (s *Service) BeginToolInvocation(
	ctx context.Context, workspaceID, conversationID, toolCallID, toolVersionID, args string,
) (string, error) {
	invocation := &domain.ToolInvocation{
		ID: util.GenerateID(), WorkspaceID: workspaceID, ConversationID: conversationID,
		ToolCallID: toolCallID, ToolVersionID: toolVersionID, Args: args,
		Result: "{}", Status: "running", CreatedAt: time.Now(),
	}
	if err := s.store.CreateInvocation(ctx, invocation); err != nil {
		return "", fmt.Errorf("create tool invocation: %w", err)
	}
	return invocation.ID, nil
}

func (s *Service) CompleteToolInvocation(
	ctx context.Context, invocationID, result string, latencyMS int, runErr error,
) error {
	status := "success"
	errorMessage := ""
	if runErr != nil {
		status = "error"
		errorMessage = runErr.Error()
	}
	return s.store.CompleteInvocation(ctx, invocationID, result, status, latencyMS, errorMessage)
}

func (s *Service) RecordSandboxExecution(ctx context.Context, execution *domain.SandboxExecution) error {
	if execution.ID == "" {
		execution.ID = util.GenerateID()
	}
	if execution.CreatedAt.IsZero() {
		execution.CreatedAt = time.Now()
	}
	return s.store.CreateSandboxExecution(ctx, execution)
}

func (s *Service) HTTPClient(timeout time.Duration) *http.Client {
	return s.policy.HTTPClient(timeout)
}

// CreateToolRequest 创建工具请求
type CreateToolRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	SourceType     string `json:"source_type"`
	Description    string `json:"description"`
	SchemaJSON     string `json:"schema_json"`
	EndpointConfig string `json:"endpoint_config"`
	AuthConfig     string `json:"auth_config"`
	Sensitive      bool   `json:"sensitive"` // 敏感工具调用前需审批
	CreatedBy      string `json:"created_by"`
}

// CreateTool 创建工具
func (s *Service) CreateTool(ctx context.Context, req CreateToolRequest) (*domain.Tool, error) {
	req.Name = strings.TrimSpace(req.Name)
	if !toolNamePattern.MatchString(req.Name) {
		return nil, fmt.Errorf("tool name must match [A-Za-z0-9_-]{1,64}")
	}
	if !isValidSourceType(req.SourceType) {
		return nil, fmt.Errorf("invalid source_type: %s", req.SourceType)
	}
	cfg, err := toolConfigFromStrings(req.SourceType, req.SchemaJSON, req.EndpointConfig, req.AuthConfig)
	if err != nil {
		return nil, err
	}
	if err := s.validateEndpoint(ctx, cfg); err != nil {
		return nil, err
	}

	tool := &domain.Tool{
		ID:          util.GenerateID(),
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		SourceType:  req.SourceType,
		Description: req.Description,
		Sensitive:   req.Sensitive,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	version := &domain.ToolVersion{
		ID:             util.GenerateID(),
		ToolID:         tool.ID,
		Version:        1,
		SchemaJSON:     req.SchemaJSON,
		EndpointConfig: req.EndpointConfig,
		AuthConfig:     req.AuthConfig,
		RetryPolicy:    `{"max_attempts": 3, "backoff_ms": 1000}`,
		Status:         "draft",
		CreatedBy:      req.CreatedBy,
		CreatedAt:      time.Now(),
	}
	if err := s.prepareVersionAuth(version); err != nil {
		return nil, err
	}

	if bundle, ok := s.store.(toolBundleStore); ok {
		if err := bundle.CreateToolWithVersion(ctx, tool, version); err != nil {
			return nil, fmt.Errorf("create tool and version: %w", err)
		}
		return tool, nil
	}
	if err := s.store.CreateTool(ctx, tool); err != nil {
		return nil, fmt.Errorf("create tool: %w", err)
	}
	if err := s.store.CreateToolVersion(ctx, version); err != nil {
		return nil, fmt.Errorf("create tool version: %w", err)
	}

	return tool, nil
}

// RecordTestRun 记录一次工具试调结果。真正的执行在 Runtime（tooling.Registry）里
// 发生——CRUD 层不导入执行层（避免 import 环），只负责落账。Publish 门禁据此放行。
func (s *Service) RecordTestRun(ctx context.Context, toolID, input, output, status string, latencyMs int, runErr error) (*domain.ToolTestRun, error) {
	version, err := s.store.GetToolCurrentVersion(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("get tested tool version: %w", err)
	}
	run := &domain.ToolTestRun{
		ID: util.GenerateID(), ToolID: toolID, ToolVersionID: version.ID,
		Input: input, Output: output, Status: status, LatencyMs: latencyMs, CreatedAt: time.Now(),
	}
	if runErr != nil {
		msg := runErr.Error()
		run.Error = &msg
	}
	if err := s.store.CreateTestRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create test run: %w", err)
	}
	return run, nil
}

// PublishTool 发布工具版本
func (s *Service) PublishTool(ctx context.Context, toolID string) error {
	version, err := s.store.GetToolCurrentVersion(ctx, toolID)
	if err != nil {
		return fmt.Errorf("get tool current version: %w", err)
	}

	if version.Status != "draft" {
		return fmt.Errorf("tool version is not in draft status")
	}

	// 检查是否有成功的测试运行
	return s.PublishToolVersion(ctx, toolID, version.ID)
}

// PublishToolVersion 发布指定版本，发布门禁只接受该版本自己的成功试调记录。
func (s *Service) PublishToolVersion(ctx context.Context, toolID, versionID string) error {
	version, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.ToolID != toolID {
		return fmt.Errorf("tool version belongs to another tool")
	}
	if version.Status != "draft" {
		return fmt.Errorf("tool version is not in draft status")
	}
	lastTest, err := s.store.GetToolLastSuccessfulTestRunForVersion(ctx, versionID)
	if err != nil || lastTest == nil {
		return fmt.Errorf("tool version must have at least one successful test run before publishing")
	}

	// 检查测试是否太旧（7天内）
	if time.Since(lastTest.CreatedAt) > 7*24*time.Hour {
		return fmt.Errorf("test run is too old, please run a new test")
	}

	return s.store.UpdateToolVersionStatus(ctx, version.ID, "published")
}

// ListToolVersions 列出一个 Tool 的全部不可变版本。
func (s *Service) ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error) {
	versions, err := s.store.ListToolVersions(ctx, toolID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.ToolVersion, 0, len(versions))
	for _, version := range versions {
		clone := *version
		clone.HasAuth = len(version.AuthConfigEncrypted) > 0 || strings.TrimSpace(version.AuthConfig) != "" && strings.TrimSpace(version.AuthConfig) != "{}"
		clone.AuthConfig = "{}"
		clone.AuthConfigEncrypted = nil
		out = append(out, &clone)
	}
	return out, nil
}

// EnsureToolWorkspace 校验 Tool 属于当前工作空间。
func (s *Service) EnsureToolWorkspace(ctx context.Context, toolID, workspaceID string) error {
	tool, err := s.store.GetTool(ctx, toolID)
	if err != nil || tool.WorkspaceID != workspaceID {
		return fmt.Errorf("tool not found")
	}
	return nil
}

// ListTools 列出工具
func (s *Service) ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error) {
	return s.store.ListTools(ctx, workspaceID)
}

// ToolExistsByName 校验某 workspace 下是否存在指定名字的工具（供 Skill 发布校验）。
func (s *Service) ToolExistsByName(ctx context.Context, workspaceID, name string) (bool, error) {
	tools, err := s.store.ListTools(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	for _, t := range tools {
		if t.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// isValidSourceType 验证source type
func isValidSourceType(sourceType string) bool {
	validTypes := []string{"rest_api", "mcp_server", "internal_sdk", "code_execution", "a2a"}
	for _, t := range validTypes {
		if t == sourceType {
			return true
		}
	}
	return false
}

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ToolConfig 工具配置结构
type ToolConfig struct {
	SourceType     string         `json:"source_type"`
	Schema         map[string]any `json:"schema"`
	EndpointConfig map[string]any `json:"endpoint_config"`
	AuthConfig     map[string]any `json:"auth_config"`
}

// GetToolMeta 返回工具的元数据（名称/描述/source_type）。
func (s *Service) GetToolMeta(ctx context.Context, toolID string) (*domain.Tool, error) {
	return s.store.GetTool(ctx, toolID)
}

// GetToolConfig 获取当前版本的工具配置（用于 Tool Executor）。
//
// 这里不强制 published：试调（Sandbox 测试场）需要对 draft 版本构造执行器。
// "未经测试不得发布"的门禁放在 PublishTool 里（按数据层校验，见讲义 §14.2）。
func (s *Service) GetToolConfig(ctx context.Context, toolID string) (*ToolConfig, error) {
	version, err := s.store.GetToolCurrentVersion(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("get tool current version: %w", err)
	}

	tool, err := s.store.GetTool(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("get tool: %w", err)
	}

	auth, err := s.authPlain(version)
	if err != nil {
		return nil, err
	}
	cfg, err := toolConfigFromStrings(tool.SourceType, version.SchemaJSON, version.EndpointConfig, auth)
	if err != nil {
		return nil, err
	}
	if err := s.validateEndpoint(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// GetToolCurrentVersionID 返回工具当前版本的 ID。
//
// Agent 快照在【创建那一刻】用它把"工具注册 ID"解析成"当前版本 ID"写死,
// 从此该快照执行的就是这个具体版本——之后给工具发新版本不影响老快照(讲义 §14.6 不可变快照)。
func (s *Service) GetToolCurrentVersionID(ctx context.Context, toolID string) (string, error) {
	v, err := s.store.GetToolLatestPublishedVersion(ctx, toolID)
	if err != nil {
		// 兼容尚未发布的早期资源与纯内存教学测试。
		v, err = s.store.GetToolCurrentVersion(ctx, toolID)
	}
	if err != nil {
		return "", fmt.Errorf("get tool current version: %w", err)
	}
	return v.ID, nil
}

// GetToolCurrentVersion 返回工具当前版本及其发布状态，供课程预设和控制面门禁检查。
func (s *Service) GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	v, err := s.store.GetToolCurrentVersion(ctx, toolID)
	if err != nil {
		return nil, fmt.Errorf("get tool current version: %w", err)
	}
	return v, nil
}

// GetToolIDByVersion 把已固化的工具版本反解为注册 ID，供控制面编辑旧 Agent 版本。
func (s *Service) GetToolIDByVersion(ctx context.Context, versionID string) (string, error) {
	v, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return "", fmt.Errorf("get tool version: %w", err)
	}
	return v.ToolID, nil
}

// GetToolConfigByVersion 取【指定版本】的工具配置(用于按 pinned 快照执行,绝不回退到 current)。
// 同时返回该版本所属的 toolID,供调用方取工具元数据(名称/描述/敏感标记)。
func (s *Service) GetToolConfigByVersion(ctx context.Context, versionID string) (*ToolConfig, string, error) {
	version, err := s.store.GetToolVersion(ctx, versionID)
	if err != nil {
		return nil, "", fmt.Errorf("get tool version: %w", err)
	}
	tool, err := s.store.GetTool(ctx, version.ToolID)
	if err != nil {
		return nil, "", fmt.Errorf("get tool: %w", err)
	}
	auth, err := s.authPlain(version)
	if err != nil {
		return nil, "", err
	}
	config, err := toolConfigFromStrings(tool.SourceType, version.SchemaJSON, version.EndpointConfig, auth)
	if err != nil {
		return nil, "", err
	}
	if err := s.validateEndpoint(ctx, config); err != nil {
		return nil, "", err
	}
	return config, version.ToolID, nil
}

func (s *Service) prepareVersionAuth(version *domain.ToolVersion) error {
	plain := strings.TrimSpace(version.AuthConfig)
	version.HasAuth = plain != "" && plain != "{}"
	if !version.HasAuth || s.cipher == nil {
		return nil
	}
	ciphertext, err := s.cipher.Encrypt(version.AuthConfig)
	if err != nil {
		return fmt.Errorf("encrypt tool auth config: %w", err)
	}
	version.AuthConfigEncrypted = ciphertext
	version.AuthConfig = "{}"
	return nil
}

func (s *Service) authPlain(version *domain.ToolVersion) (string, error) {
	if len(version.AuthConfigEncrypted) == 0 {
		return version.AuthConfig, nil
	}
	if s.cipher == nil {
		return "", fmt.Errorf("tool credential cipher is not configured")
	}
	plain, err := s.cipher.Decrypt(version.AuthConfigEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt tool auth config: %w", err)
	}
	return plain, nil
}

func (s *Service) validateEndpoint(ctx context.Context, cfg *ToolConfig) error {
	if s.policy == nil {
		return nil
	}
	return s.policy.validateConfig(ctx, cfg.SourceType, cfg.EndpointConfig)
}

func toolConfigFromStrings(sourceType, schemaJSON, endpointJSON, authJSON string) (*ToolConfig, error) {
	schema, err := parseJSONObject("schema_json", schemaJSON)
	if err != nil {
		return nil, err
	}
	endpoint, err := parseJSONObject("endpoint_config", endpointJSON)
	if err != nil {
		return nil, err
	}
	auth, err := parseJSONObject("auth_config", authJSON)
	if err != nil {
		return nil, err
	}
	return &ToolConfig{SourceType: sourceType, Schema: schema, EndpointConfig: endpoint, AuthConfig: auth}, nil
}

func parseJSONObject(field, value string) (map[string]any, error) {
	if value == "" {
		return map[string]any{}, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", field, err)
	}
	if object == nil {
		return nil, fmt.Errorf("invalid %s: expected JSON object", field)
	}
	return object, nil
}
