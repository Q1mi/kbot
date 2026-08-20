// Package modelconfig 管理模型部署配置版本。
package modelconfig

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
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
	ID                string       `json:"id"`
	WorkspaceID       string       `json:"workspace_id"`
	Name              string       `json:"name"`
	ClassificationMax string       `json:"classification_max"`
	Deployments       []Deployment `json:"deployments"`
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
			result = append(result, profile)
		}
	}
	return result
}

type Registry struct {
	mu       sync.RWMutex
	profiles map[string]ProfileVersion
	aead     cipher.AEAD
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
	return &Registry{profiles: make(map[string]ProfileVersion), aead: aead}
}

func (r *Registry) Publish(_ context.Context, profile ProfileVersion) error {
	if profile.ID == "" || profile.WorkspaceID == "" || len(profile.Deployments) == 0 {
		return fmt.Errorf("id, workspace and deployments are required")
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
	r.profiles[profile.ID] = profile
	return nil
}

func (r *Registry) Resolve(_ context.Context, workspaceID, versionID string) (ProfileVersion, error) {
	r.mu.RLock()
	profile, ok := r.profiles[versionID]
	r.mu.RUnlock()
	if !ok || profile.WorkspaceID != workspaceID {
		return ProfileVersion{}, fmt.Errorf("model profile %s not found", versionID)
	}
	profile.Deployments = append([]Deployment(nil), profile.Deployments...)
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
