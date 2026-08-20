// Package modelconfig 管理模型部署配置版本。
package modelconfig

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Deployment struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url"`
	MaxRetries int    `json:"max_retries"`
	APIKey     string `json:"-"`
	HasAPIKey  bool   `json:"has_api_key"`

	apiKeyCiphertext []byte
}
type ProfileVersion struct {
	ID                    string       `json:"id"`
	ProfileID             string       `json:"profile_id"`
	Version               int          `json:"version"`
	WorkspaceID           string       `json:"workspace_id"`
	Name                  string       `json:"name"`
	ClassificationMax     string       `json:"classification_max"`
	PrimaryDeploymentID   string       `json:"primary_deployment_id"`
	FallbackDeploymentIDs []string     `json:"fallback_deployment_ids"`
	Deployments           []Deployment `json:"deployments"`
}

type ProviderAccount struct {
	ID, WorkspaceID, Name, Kind, BaseURL string
	Status                               string
	HasAPIKey                            bool
	apiKeyCiphertext                     []byte
}

func (a ProviderAccount) MarshalJSON() ([]byte, error) {
	type view struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		BaseURL   string `json:"base_url"`
		HasAPIKey bool   `json:"has_api_key"`
	}
	return json.Marshal(view{ID: a.ID, Name: a.Name, Kind: a.Kind, Status: a.Status, BaseURL: a.BaseURL, HasAPIKey: a.HasAPIKey})
}

type ModelDeployment struct {
	ID, WorkspaceID, ProviderAccountID, Name, ModelName, Region, Status string
	TimeoutMS, MaxRetries                                               int
}

func (d ModelDeployment) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": d.ID, "provider_account_id": d.ProviderAccountID, "name": d.Name, "model_name": d.ModelName, "region": d.Region, "timeout_ms": d.TimeoutMS, "max_retries": d.MaxRetries, "status": d.Status})
}

type Profile struct {
	ID, WorkspaceID, Name, Description string
	CreatedAt                          time.Time
}

func (p Profile) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": p.ID, "name": p.Name, "description": p.Description, "created_at": p.CreatedAt})
}

func (r *Registry) Validate(ctx context.Context, workspaceID, versionID string) error {
	_, err := r.Resolve(ctx, workspaceID, versionID)
	return err
}

func (r *Registry) List(_ context.Context, workspaceID string) []ProfileVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProfileVersion, 0, len(r.profiles))
	for _, profile := range r.profiles {
		if profile.WorkspaceID == workspaceID {
			profile.Deployments = append([]Deployment(nil), profile.Deployments...)
			for index := range profile.Deployments {
				profile.Deployments[index].APIKey = ""
				profile.Deployments[index].apiKeyCiphertext = nil
			}
			profile.FallbackDeploymentIDs = append([]string(nil), profile.FallbackDeploymentIDs...)
			result = append(result, profile)
		}
	}
	return result
}

type Registry struct {
	mu          sync.RWMutex
	profiles    map[string]ProfileVersion
	accounts    map[string]ProviderAccount
	deployments map[string]ModelDeployment
	profileDefs map[string]Profile
	sequence    atomic.Uint64
	aead        cipher.AEAD
}

func NewRegistry(credentialKeys ...[]byte) *Registry {
	key := make([]byte, 32)
	if len(credentialKeys) > 0 && len(credentialKeys[0]) > 0 {
		digest := sha256.Sum256(credentialKeys[0])
		copy(key, digest[:])
	} else if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic(fmt.Errorf("generate model credential key: %w", err))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &Registry{
		profiles: make(map[string]ProfileVersion), accounts: make(map[string]ProviderAccount),
		deployments: make(map[string]ModelDeployment), profileDefs: make(map[string]Profile), aead: aead,
	}
}

func (r *Registry) Publish(_ context.Context, profile ProfileVersion) error {
	if profile.ID == "" || profile.WorkspaceID == "" || len(profile.Deployments) == 0 {
		return fmt.Errorf("id, workspace and deployments are required")
	}
	if profile.ProfileID == "" {
		profile.ProfileID = profile.ID
	}
	if profile.Version <= 0 {
		profile.Version = 1
	}
	if !validClassification(profile.ClassificationMax) {
		return fmt.Errorf("invalid classification %q", profile.ClassificationMax)
	}
	profile.Deployments = append([]Deployment(nil), profile.Deployments...)
	for index := range profile.Deployments {
		deployment := &profile.Deployments[index]
		parsed, err := url.Parse(deployment.BaseURL)
		if deployment.Provider == "" || deployment.Model == "" || err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("invalid deployment")
		}
		ciphertext, err := r.encrypt(deployment.APIKey)
		if err != nil {
			return fmt.Errorf("encrypt deployment credentials: %w", err)
		}
		deployment.HasAPIKey = len(ciphertext) > 0
		deployment.APIKey = ""
		deployment.apiKeyCiphertext = ciphertext
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.profiles[profile.ID]; exists {
		return fmt.Errorf("model profile %s already exists", profile.ID)
	}
	profile.Deployments = append([]Deployment(nil), profile.Deployments...)
	profile.FallbackDeploymentIDs = append([]string(nil), profile.FallbackDeploymentIDs...)
	r.profiles[profile.ID] = profile
	if _, exists := r.profileDefs[profile.ProfileID]; !exists {
		r.profileDefs[profile.ProfileID] = Profile{ID: profile.ProfileID, WorkspaceID: profile.WorkspaceID, Name: profile.Name, CreatedAt: time.Now().UTC()}
	}
	return nil
}

func (r *Registry) CreateAccount(workspaceID, name, kind, baseURL, apiKey string) (ProviderAccount, error) {
	parsed, err := url.Parse(baseURL)
	if workspaceID == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(kind) == "" || err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ProviderAccount{}, fmt.Errorf("workspace, name, provider kind and absolute base URL are required")
	}
	ciphertext, err := r.encrypt(apiKey)
	if err != nil {
		return ProviderAccount{}, fmt.Errorf("encrypt provider credential: %w", err)
	}
	item := ProviderAccount{ID: fmt.Sprintf("model-account-%d", r.sequence.Add(1)), WorkspaceID: workspaceID, Name: strings.TrimSpace(name), Kind: kind, BaseURL: baseURL, Status: "active", HasAPIKey: len(ciphertext) > 0, apiKeyCiphertext: ciphertext}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.accounts {
		if existing.WorkspaceID == workspaceID && existing.Name == item.Name {
			return ProviderAccount{}, fmt.Errorf("provider account name already exists")
		}
	}
	r.accounts[item.ID] = item
	return item, nil
}

func (r *Registry) ListAccounts(workspaceID string) []ProviderAccount {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ProviderAccount, 0)
	for _, item := range r.accounts {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	return result
}

func (r *Registry) RotateAPIKey(workspaceID, accountID, apiKey string) error {
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("api key is required")
	}
	ciphertext, err := r.encrypt(apiKey)
	if err != nil {
		return fmt.Errorf("encrypt provider credential: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.accounts[accountID]
	if !ok || item.WorkspaceID != workspaceID {
		return fmt.Errorf("provider account not found")
	}
	item.HasAPIKey = true
	item.apiKeyCiphertext = ciphertext
	r.accounts[item.ID] = item
	return nil
}

func (r *Registry) CreateDeployment(workspaceID, accountID, name, modelName, region string, timeoutMS, maxRetries int) (ModelDeployment, error) {
	if timeoutMS <= 0 {
		timeoutMS = 120000
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(modelName) == "" || maxRetries < 0 {
		return ModelDeployment{}, fmt.Errorf("deployment name, model and valid retry policy are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[accountID]
	if !ok || account.WorkspaceID != workspaceID {
		return ModelDeployment{}, fmt.Errorf("provider account not found")
	}
	item := ModelDeployment{ID: fmt.Sprintf("model-deployment-%d", r.sequence.Add(1)), WorkspaceID: workspaceID, ProviderAccountID: accountID, Name: name, ModelName: modelName, Region: region, TimeoutMS: timeoutMS, MaxRetries: maxRetries, Status: "active"}
	r.deployments[item.ID] = item
	return item, nil
}

func (r *Registry) ListDeployments(workspaceID string) []ModelDeployment {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ModelDeployment, 0)
	for _, item := range r.deployments {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	return result
}

func (r *Registry) CreateProfile(ctx context.Context, workspaceID, name, description, primaryID string, fallbackIDs []string, classification string) (Profile, ProfileVersion, error) {
	profile := Profile{ID: fmt.Sprintf("model-profile-%d", r.sequence.Add(1)), WorkspaceID: workspaceID, Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), CreatedAt: time.Now().UTC()}
	if profile.Name == "" {
		return Profile{}, ProfileVersion{}, fmt.Errorf("profile name is required")
	}
	r.mu.RLock()
	for _, existing := range r.profileDefs {
		if existing.WorkspaceID == workspaceID && existing.Name == profile.Name {
			r.mu.RUnlock()
			return Profile{}, ProfileVersion{}, fmt.Errorf("profile name already exists")
		}
	}
	r.mu.RUnlock()
	version, err := r.buildProfileVersion(workspaceID, profile.ID, profile.Name, 1, primaryID, fallbackIDs, classification)
	if err != nil {
		return Profile{}, ProfileVersion{}, err
	}
	if err := r.Publish(ctx, version); err != nil {
		return Profile{}, ProfileVersion{}, err
	}
	r.mu.Lock()
	r.profileDefs[profile.ID] = profile
	r.mu.Unlock()
	return profile, version, nil
}

func (r *Registry) CreateProfileVersion(ctx context.Context, workspaceID, profileID, primaryID string, fallbackIDs []string, classification string) (ProfileVersion, error) {
	r.mu.RLock()
	profile, ok := r.profileDefs[profileID]
	count := 0
	for _, version := range r.profiles {
		if version.ProfileID == profileID {
			count++
		}
	}
	r.mu.RUnlock()
	if !ok || profile.WorkspaceID != workspaceID {
		return ProfileVersion{}, fmt.Errorf("model profile not found")
	}
	version, err := r.buildProfileVersion(workspaceID, profileID, profile.Name, count+1, primaryID, fallbackIDs, classification)
	if err != nil {
		return ProfileVersion{}, err
	}
	return version, r.Publish(ctx, version)
}

func (r *Registry) buildProfileVersion(workspaceID, profileID, name string, number int, primaryID string, fallbackIDs []string, classification string) (ProfileVersion, error) {
	if classification == "" {
		classification = "internal"
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := append([]string{primaryID}, fallbackIDs...)
	deployments := make([]Deployment, 0, len(ids))
	for _, id := range ids {
		item, ok := r.deployments[id]
		account := r.accounts[item.ProviderAccountID]
		if !ok || item.WorkspaceID != workspaceID || account.WorkspaceID != workspaceID {
			return ProfileVersion{}, fmt.Errorf("model deployment %s not found", id)
		}
		apiKey, err := r.decrypt(account.apiKeyCiphertext)
		if err != nil {
			return ProfileVersion{}, fmt.Errorf("decrypt provider credential: %w", err)
		}
		deployments = append(deployments, Deployment{Provider: account.Kind, Model: item.ModelName, BaseURL: account.BaseURL, APIKey: apiKey})
	}
	return ProfileVersion{ID: fmt.Sprintf("model-profile-version-%d", r.sequence.Add(1)), ProfileID: profileID, Version: number, WorkspaceID: workspaceID, Name: name, ClassificationMax: classification, PrimaryDeploymentID: primaryID, FallbackDeploymentIDs: append([]string(nil), fallbackIDs...), Deployments: deployments}, nil
}

func (r *Registry) ListProfileDefinitions(workspaceID string) []Profile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Profile, 0)
	for _, item := range r.profileDefs {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	return result
}

func (r *Registry) Resolve(_ context.Context, workspaceID, versionID string) (ProfileVersion, error) {
	r.mu.RLock()
	profile, ok := r.profiles[versionID]
	r.mu.RUnlock()
	if !ok || profile.WorkspaceID != workspaceID {
		return ProfileVersion{}, fmt.Errorf("model profile %s not found", versionID)
	}
	profile.Deployments = append([]Deployment(nil), profile.Deployments...)
	profile.FallbackDeploymentIDs = append([]string(nil), profile.FallbackDeploymentIDs...)
	for index := range profile.Deployments {
		plaintext, err := r.decrypt(profile.Deployments[index].apiKeyCiphertext)
		if err != nil {
			return ProfileVersion{}, fmt.Errorf("decrypt deployment credentials: %w", err)
		}
		profile.Deployments[index].APIKey = plaintext
		profile.Deployments[index].apiKeyCiphertext = nil
	}
	return profile, nil
}

func (r *Registry) encrypt(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, r.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return r.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (r *Registry) decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	nonceSize := r.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("invalid API key ciphertext")
	}
	plaintext, err := r.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func validClassification(value string) bool {
	switch strings.ToLower(value) {
	case "public", "internal", "confidential", "secret":
		return true
	default:
		return false
	}
}
