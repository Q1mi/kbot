// Package modelconfig 管理模型供应商账号、实际部署、逻辑 Profile 与项目绑定。
package modelconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/llm"
	"github.com/Q1mi/kbot/internal/util"
)

// DefaultDeploymentTimeoutMS 给真实推理模型预留完整 Tool 回合的响应时间。
// 用户仍可按具体 Provider 的 SLA 在创建 Deployment 时覆盖。
const DefaultDeploymentTimeoutMS = 120000

// ErrProfileNameExists 表示同一 Workspace 中已经存在同名 Model Profile。
var ErrProfileNameExists = errors.New("model profile name already exists in workspace")

type ProviderAccount struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	BaseURL     string    `json:"base_url"`
	Status      string    `json:"status"`
	HasAPIKey   bool      `json:"has_api_key"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type providerAccountRecord struct {
	ProviderAccount
	APIKeyCiphertext []byte
}

type ModelDeployment struct {
	ID                         string    `json:"id"`
	WorkspaceID                string    `json:"workspace_id"`
	ProviderAccountID          string    `json:"provider_account_id"`
	Name                       string    `json:"name"`
	ModelName                  string    `json:"model_name"`
	Region                     string    `json:"region"`
	TimeoutMS                  int       `json:"timeout_ms"`
	MaxRetries                 int       `json:"max_retries"`
	InputPricePerMillion       float64   `json:"input_price_per_million"`
	OutputPricePerMillion      float64   `json:"output_price_per_million"`
	CachedInputPricePerMillion float64   `json:"cached_input_price_per_million"`
	Status                     string    `json:"status"`
	CreatedBy                  string    `json:"created_by"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type ModelProfile struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ModelProfileVersion struct {
	ID                    string    `json:"id"`
	ProfileID             string    `json:"profile_id"`
	Version               int       `json:"version"`
	PrimaryDeploymentID   string    `json:"primary_deployment_id"`
	FallbackDeploymentIDs []string  `json:"fallback_deployment_ids"`
	ClassificationMax     string    `json:"classification_max"`
	CreatedBy             string    `json:"created_by"`
	CreatedAt             time.Time `json:"created_at"`
}

type ProjectBinding struct {
	WorkspaceID           string  `json:"workspace_id"`
	Env                   string  `json:"env"`
	ModelProfileVersionID string  `json:"model_profile_version_id"`
	MonthlyBudget         float64 `json:"monthly_budget"`
	RPMLimit              int     `json:"rpm_limit"`
	TPMLimit              int     `json:"tpm_limit"`
}

// SeedProfile 描述课堂首启时创建的一项业务模型方案。
// 每项 Profile 使用独立 Deployment，便于后续分别调整模型、超时与重试策略。
type SeedProfile struct {
	Name              string
	Description       string
	DeploymentName    string
	ClassificationMax string
}

// SeedWorkspaceConfig 描述一个 Workspace 的专属模型控制面初始化参数。
type SeedWorkspaceConfig struct {
	WorkspaceID  string
	ProviderName string
	ProviderKind string
	BaseURL      string
	APIKey       string
	ModelName    string
	Region       string
	CreatedBy    string
	Profiles     []SeedProfile
}

type Store interface {
	CreateProviderAccount(context.Context, *providerAccountRecord) error
	UpdateProviderAPIKey(context.Context, string, []byte) error
	ListProviderAccounts(context.Context, string) ([]*providerAccountRecord, error)
	GetProviderAccount(context.Context, string) (*providerAccountRecord, error)
	CreateDeployment(context.Context, *ModelDeployment) error
	UpdateDeploymentPricing(context.Context, string, float64, float64, float64) error
	ListDeployments(context.Context, string) ([]*ModelDeployment, error)
	GetDeployment(context.Context, string) (*ModelDeployment, error)
	CreateProfile(context.Context, *ModelProfile) error
	ListProfiles(context.Context, string) ([]*ModelProfile, error)
	GetProfile(context.Context, string) (*ModelProfile, error)
	CreateProfileVersion(context.Context, *ModelProfileVersion) error
	ListProfileVersions(context.Context, string) ([]*ModelProfileVersion, error)
	GetProfileVersion(context.Context, string) (*ModelProfileVersion, error)
	UpsertProjectBinding(context.Context, *ProjectBinding) error
	GetProjectBinding(context.Context, string, string) (*ProjectBinding, error)
	ReserveProjectUsage(context.Context, llm.ProjectQuotaRequest) (string, error)
	FinalizeProjectUsage(context.Context, string, int, float64, bool) error
}

type profileBundleStore interface {
	CreateProfileWithVersion(context.Context, *ModelProfile, *ModelProfileVersion) error
}

type Service struct {
	store          Store
	cipher         *Cipher
	endpointPolicy interface {
		ValidateURL(context.Context, string) error
	}
}

func NewService(store Store, cipher *Cipher) *Service {
	return &Service{store: store, cipher: cipher}
}

func (s *Service) ConfigureEndpointPolicy(policy interface {
	ValidateURL(context.Context, string) error
}) {
	s.endpointPolicy = policy
}

type CreateProviderAccountRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	CreatedBy   string `json:"created_by"`
}

func (s *Service) CreateProviderAccount(ctx context.Context, req CreateProviderAccountRequest) (*ProviderAccount, error) {
	if req.Name == "" || req.Kind == "" || req.BaseURL == "" || req.APIKey == "" {
		return nil, fmt.Errorf("name, kind, base_url and api_key are required")
	}
	if s.endpointPolicy != nil {
		if err := s.endpointPolicy.ValidateURL(ctx, req.BaseURL); err != nil {
			return nil, fmt.Errorf("validate provider base_url: %w", err)
		}
	}
	encrypted, err := s.cipher.Encrypt(req.APIKey)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	rec := &providerAccountRecord{
		ProviderAccount: ProviderAccount{
			ID: util.GenerateID(), WorkspaceID: req.WorkspaceID, Name: req.Name,
			Kind: req.Kind, BaseURL: req.BaseURL, Status: "active", HasAPIKey: true,
			CreatedBy: req.CreatedBy, CreatedAt: now, UpdatedAt: now,
		},
		APIKeyCiphertext: encrypted,
	}
	if err := s.store.CreateProviderAccount(ctx, rec); err != nil {
		return nil, err
	}
	out := rec.ProviderAccount
	return &out, nil
}

func (s *Service) ListProviderAccounts(ctx context.Context, workspaceID string) ([]*ProviderAccount, error) {
	rows, err := s.store.ListProviderAccounts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]*ProviderAccount, 0, len(rows))
	for _, row := range rows {
		v := row.ProviderAccount
		v.HasAPIKey = len(row.APIKeyCiphertext) > 0
		out = append(out, &v)
	}
	return out, nil
}

// EnsureSeedWorkspaceConfig 幂等创建 Workspace 专属的 Provider Account、Deployments 和 Profiles。
// 已存在的同名资源保持原样，避免首启或容器重启覆盖学员后续配置。
func (s *Service) EnsureSeedWorkspaceConfig(ctx context.Context, req SeedWorkspaceConfig) error {
	if req.WorkspaceID == "" || req.ProviderName == "" || req.BaseURL == "" || req.APIKey == "" || req.ModelName == "" {
		return fmt.Errorf("workspace_id, provider_name, base_url, api_key and model_name are required")
	}
	if req.ProviderKind == "" {
		req.ProviderKind = "openai-compatible"
	}
	if req.CreatedBy == "" {
		req.CreatedBy = "system"
	}

	accounts, err := s.ListProviderAccounts(ctx, req.WorkspaceID)
	if err != nil {
		return err
	}
	var account *ProviderAccount
	for _, candidate := range accounts {
		if candidate.Name == req.ProviderName {
			account = candidate
			break
		}
	}
	if account == nil {
		account, err = s.CreateProviderAccount(ctx, CreateProviderAccountRequest{
			WorkspaceID: req.WorkspaceID, Name: req.ProviderName, Kind: req.ProviderKind,
			BaseURL: req.BaseURL, APIKey: req.APIKey, CreatedBy: req.CreatedBy,
		})
		if err != nil {
			return fmt.Errorf("create seed provider account: %w", err)
		}
	}

	deployments, err := s.ListDeployments(ctx, req.WorkspaceID)
	if err != nil {
		return err
	}
	deploymentByName := make(map[string]*ModelDeployment, len(deployments))
	for _, deployment := range deployments {
		deploymentByName[deployment.Name] = deployment
	}
	profiles, err := s.ListProfiles(ctx, req.WorkspaceID)
	if err != nil {
		return err
	}
	profileByName := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		profileByName[profile.Name] = struct{}{}
	}

	for _, seed := range req.Profiles {
		if seed.Name == "" || seed.DeploymentName == "" {
			return fmt.Errorf("seed profile name and deployment_name are required")
		}
		deployment := deploymentByName[seed.DeploymentName]
		if deployment == nil {
			deployment, err = s.CreateDeployment(ctx, CreateDeploymentRequest{
				WorkspaceID: req.WorkspaceID, ProviderAccountID: account.ID,
				Name: seed.DeploymentName, ModelName: req.ModelName, Region: req.Region,
				TimeoutMS: DefaultDeploymentTimeoutMS, MaxRetries: 0, CreatedBy: req.CreatedBy,
			})
			if err != nil {
				return fmt.Errorf("create seed deployment %q: %w", seed.DeploymentName, err)
			}
			deploymentByName[seed.DeploymentName] = deployment
		}
		if _, ok := profileByName[seed.Name]; ok {
			continue
		}
		if _, _, err := s.CreateProfile(ctx, CreateProfileRequest{
			WorkspaceID: req.WorkspaceID, Name: seed.Name, Description: seed.Description,
			PrimaryDeploymentID: deployment.ID, ClassificationMax: seed.ClassificationMax,
			CreatedBy: req.CreatedBy,
		}); err != nil {
			return fmt.Errorf("create seed profile %q: %w", seed.Name, err)
		}
		profileByName[seed.Name] = struct{}{}
	}
	return nil
}

func (s *Service) RotateProviderAPIKey(ctx context.Context, workspaceID, accountID, apiKey string) error {
	if apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	account, err := s.store.GetProviderAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if account.WorkspaceID != workspaceID {
		return fmt.Errorf("provider account belongs to another workspace")
	}
	encrypted, err := s.cipher.Encrypt(apiKey)
	if err != nil {
		return err
	}
	return s.store.UpdateProviderAPIKey(ctx, accountID, encrypted)
}

type CreateDeploymentRequest struct {
	WorkspaceID                string  `json:"workspace_id"`
	ProviderAccountID          string  `json:"provider_account_id"`
	Name                       string  `json:"name"`
	ModelName                  string  `json:"model_name"`
	Region                     string  `json:"region"`
	TimeoutMS                  int     `json:"timeout_ms"`
	MaxRetries                 int     `json:"max_retries"`
	InputPricePerMillion       float64 `json:"input_price_per_million"`
	OutputPricePerMillion      float64 `json:"output_price_per_million"`
	CachedInputPricePerMillion float64 `json:"cached_input_price_per_million"`
	CreatedBy                  string  `json:"created_by"`
}

func (s *Service) CreateDeployment(ctx context.Context, req CreateDeploymentRequest) (*ModelDeployment, error) {
	account, err := s.store.GetProviderAccount(ctx, req.ProviderAccountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != req.WorkspaceID {
		return nil, fmt.Errorf("provider account belongs to another workspace")
	}
	if req.Name == "" || req.ModelName == "" {
		return nil, fmt.Errorf("name and model_name are required")
	}
	if req.TimeoutMS <= 0 {
		req.TimeoutMS = DefaultDeploymentTimeoutMS
	}
	if req.MaxRetries < 0 {
		req.MaxRetries = 0
	}
	if req.InputPricePerMillion < 0 || req.OutputPricePerMillion < 0 || req.CachedInputPricePerMillion < 0 {
		return nil, fmt.Errorf("model prices cannot be negative")
	}
	now := time.Now()
	d := &ModelDeployment{
		ID: util.GenerateID(), WorkspaceID: req.WorkspaceID, ProviderAccountID: req.ProviderAccountID,
		Name: req.Name, ModelName: req.ModelName, Region: req.Region, TimeoutMS: req.TimeoutMS,
		MaxRetries: req.MaxRetries, Status: "active", CreatedBy: req.CreatedBy,
		InputPricePerMillion: req.InputPricePerMillion, OutputPricePerMillion: req.OutputPricePerMillion,
		CachedInputPricePerMillion: req.CachedInputPricePerMillion,
		CreatedAt:                  now, UpdatedAt: now,
	}
	if err := s.store.CreateDeployment(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) ListDeployments(ctx context.Context, workspaceID string) ([]*ModelDeployment, error) {
	return s.store.ListDeployments(ctx, workspaceID)
}

type UpdateDeploymentPricingRequest struct {
	WorkspaceID                string  `json:"workspace_id"`
	InputPricePerMillion       float64 `json:"input_price_per_million"`
	OutputPricePerMillion      float64 `json:"output_price_per_million"`
	CachedInputPricePerMillion float64 `json:"cached_input_price_per_million"`
}

func (s *Service) UpdateDeploymentPricing(
	ctx context.Context, deploymentID string, req UpdateDeploymentPricingRequest,
) (*ModelDeployment, error) {
	if req.InputPricePerMillion < 0 || req.OutputPricePerMillion < 0 || req.CachedInputPricePerMillion < 0 {
		return nil, fmt.Errorf("model prices cannot be negative")
	}
	deployment, err := s.store.GetDeployment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	if deployment.WorkspaceID != req.WorkspaceID {
		return nil, fmt.Errorf("model deployment not found")
	}
	if err := s.store.UpdateDeploymentPricing(
		ctx, deploymentID, req.InputPricePerMillion, req.OutputPricePerMillion,
		req.CachedInputPricePerMillion,
	); err != nil {
		return nil, err
	}
	deployment.InputPricePerMillion = req.InputPricePerMillion
	deployment.OutputPricePerMillion = req.OutputPricePerMillion
	deployment.CachedInputPricePerMillion = req.CachedInputPricePerMillion
	deployment.UpdatedAt = time.Now()
	return deployment, nil
}

type CreateProfileRequest struct {
	WorkspaceID           string   `json:"workspace_id"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	PrimaryDeploymentID   string   `json:"primary_deployment_id"`
	FallbackDeploymentIDs []string `json:"fallback_deployment_ids"`
	ClassificationMax     string   `json:"classification_max"`
	CreatedBy             string   `json:"created_by"`
}

func (s *Service) CreateProfile(ctx context.Context, req CreateProfileRequest) (*ModelProfile, *ModelProfileVersion, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.PrimaryDeploymentID == "" {
		return nil, nil, fmt.Errorf("name and primary_deployment_id are required")
	}
	profiles, err := s.store.ListProfiles(ctx, req.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	for _, profile := range profiles {
		if profile.Name == req.Name {
			return nil, nil, ErrProfileNameExists
		}
	}
	if err := s.validateDeployments(ctx, req.WorkspaceID, append([]string{req.PrimaryDeploymentID}, req.FallbackDeploymentIDs...)); err != nil {
		return nil, nil, err
	}
	if req.ClassificationMax == "" {
		req.ClassificationMax = "internal"
	}
	now := time.Now()
	p := &ModelProfile{
		ID: util.GenerateID(), WorkspaceID: req.WorkspaceID, Name: req.Name,
		Description: req.Description, CreatedBy: req.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	v := &ModelProfileVersion{
		ID: util.GenerateID(), ProfileID: p.ID, Version: 1,
		PrimaryDeploymentID: req.PrimaryDeploymentID, FallbackDeploymentIDs: req.FallbackDeploymentIDs,
		ClassificationMax: req.ClassificationMax, CreatedBy: req.CreatedBy, CreatedAt: now,
	}
	if bundle, ok := s.store.(profileBundleStore); ok {
		if err := bundle.CreateProfileWithVersion(ctx, p, v); err != nil {
			return nil, nil, err
		}
		return p, v, nil
	}
	if err := s.store.CreateProfile(ctx, p); err != nil {
		return nil, nil, err
	}
	if err := s.store.CreateProfileVersion(ctx, v); err != nil {
		return nil, nil, err
	}
	return p, v, nil
}

func (s *Service) CreateProfileVersion(ctx context.Context, profileID string, primary string, fallbacks []string, classification, actor string) (*ModelProfileVersion, error) {
	p, err := s.store.GetProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if err := s.validateDeployments(ctx, p.WorkspaceID, append([]string{primary}, fallbacks...)); err != nil {
		return nil, err
	}
	versions, err := s.store.ListProfileVersions(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if classification == "" {
		classification = "internal"
	}
	v := &ModelProfileVersion{
		ID: util.GenerateID(), ProfileID: profileID, Version: len(versions) + 1,
		PrimaryDeploymentID: primary, FallbackDeploymentIDs: fallbacks,
		ClassificationMax: classification, CreatedBy: actor, CreatedAt: time.Now(),
	}
	if err := s.store.CreateProfileVersion(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Service) ListProfiles(ctx context.Context, workspaceID string) ([]*ModelProfile, error) {
	return s.store.ListProfiles(ctx, workspaceID)
}

func (s *Service) ListProfileVersions(ctx context.Context, profileID string) ([]*ModelProfileVersion, error) {
	return s.store.ListProfileVersions(ctx, profileID)
}

func (s *Service) EnsureProfileWorkspace(ctx context.Context, profileID, workspaceID string) error {
	profile, err := s.store.GetProfile(ctx, profileID)
	if err != nil || profile.WorkspaceID != workspaceID {
		return fmt.Errorf("model profile not found")
	}
	return nil
}

func (s *Service) ListWorkspaceProfileVersions(ctx context.Context, workspaceID string) ([]*ModelProfileVersion, error) {
	profiles, err := s.store.ListProfiles(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var out []*ModelProfileVersion
	for _, profile := range profiles {
		versions, err := s.store.ListProfileVersions(ctx, profile.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, versions...)
	}
	return out, nil
}

func (s *Service) ValidateProfileVersion(ctx context.Context, workspaceID, versionID string) error {
	if versionID == "" {
		return nil
	}
	v, err := s.store.GetProfileVersion(ctx, versionID)
	if err != nil {
		return err
	}
	p, err := s.store.GetProfile(ctx, v.ProfileID)
	if err != nil {
		return err
	}
	if p.WorkspaceID != workspaceID {
		return fmt.Errorf("model profile version belongs to another workspace")
	}
	return nil
}

func (s *Service) BindProject(ctx context.Context, b *ProjectBinding) error {
	if b.WorkspaceID == "" || b.Env == "" || b.ModelProfileVersionID == "" {
		return fmt.Errorf("workspace_id, env and model_profile_version_id are required")
	}
	if b.MonthlyBudget < 0 || b.RPMLimit < 0 || b.TPMLimit < 0 {
		return fmt.Errorf("budget and rate limits cannot be negative")
	}
	if err := s.ValidateProfileVersion(ctx, b.WorkspaceID, b.ModelProfileVersionID); err != nil {
		return err
	}
	if b.MonthlyBudget > 0 {
		version, err := s.store.GetProfileVersion(ctx, b.ModelProfileVersionID)
		if err != nil {
			return err
		}
		deploymentIDs := append([]string{version.PrimaryDeploymentID}, version.FallbackDeploymentIDs...)
		seen := make(map[string]struct{}, len(deploymentIDs))
		for _, deploymentID := range deploymentIDs {
			if _, ok := seen[deploymentID]; ok {
				continue
			}
			seen[deploymentID] = struct{}{}
			deployment, err := s.store.GetDeployment(ctx, deploymentID)
			if err != nil {
				return err
			}
			if deployment.InputPricePerMillion == 0 && deployment.OutputPricePerMillion == 0 {
				return fmt.Errorf("monthly budget requires token prices on deployment %q", deployment.Name)
			}
		}
	}
	return s.store.UpsertProjectBinding(ctx, b)
}

func (s *Service) GetProjectBinding(ctx context.Context, workspaceID, env string) (*ProjectBinding, error) {
	if env == "" {
		env = "dev"
	}
	return s.store.GetProjectBinding(ctx, workspaceID, env)
}

func (s *Service) ReserveProjectUsage(ctx context.Context, req llm.ProjectQuotaRequest) (string, error) {
	return s.store.ReserveProjectUsage(ctx, req)
}

func (s *Service) FinalizeProjectUsage(ctx context.Context, reservationID string, actualTokens int, actualCost float64, success bool) error {
	return s.store.FinalizeProjectUsage(ctx, reservationID, actualTokens, actualCost, success)
}

func (s *Service) ResolveProfile(ctx context.Context, versionID string) (*llm.ResolvedModelProfile, error) {
	v, err := s.store.GetProfileVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{v.PrimaryDeploymentID}, v.FallbackDeploymentIDs...)
	out := &llm.ResolvedModelProfile{
		VersionID: versionID, ClassificationMax: v.ClassificationMax,
		Deployments: make([]llm.ResolvedDeployment, 0, len(ids)),
	}
	for _, id := range ids {
		d, err := s.store.GetDeployment(ctx, id)
		if err != nil {
			return nil, err
		}
		a, err := s.store.GetProviderAccount(ctx, d.ProviderAccountID)
		if err != nil {
			return nil, err
		}
		if s.endpointPolicy != nil {
			if err := s.endpointPolicy.ValidateURL(ctx, a.BaseURL); err != nil {
				return nil, fmt.Errorf("validate provider endpoint: %w", err)
			}
		}
		apiKey, err := s.cipher.Decrypt(a.APIKeyCiphertext)
		if err != nil {
			return nil, err
		}
		out.Deployments = append(out.Deployments, llm.ResolvedDeployment{
			ID: d.ID, ProviderID: a.ID, ProviderKind: a.Kind, BaseURL: a.BaseURL,
			APIKey: apiKey, Model: d.ModelName, TimeoutMS: d.TimeoutMS, MaxRetries: d.MaxRetries,
			InputPricePerMillion: d.InputPricePerMillion, OutputPricePerMillion: d.OutputPricePerMillion,
			CachedInputPricePerMillion: d.CachedInputPricePerMillion,
		})
	}
	return out, nil
}

func (s *Service) validateDeployments(ctx context.Context, workspaceID string, ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		d, err := s.store.GetDeployment(ctx, id)
		if err != nil {
			return err
		}
		if d.WorkspaceID != workspaceID {
			return fmt.Errorf("deployment %s belongs to another workspace", id)
		}
	}
	return nil
}

func encodeStrings(v []string) []byte {
	b, _ := json.Marshal(v)
	return b
}
