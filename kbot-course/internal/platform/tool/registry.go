// Package tool 管理可被 Agent 固定引用的工具版本。
package tool

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type Version struct {
	ID          string    `json:"id"`
	ToolID      string    `json:"tool_id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	SourceType  string    `json:"source_type"`
	Description string    `json:"description"`
	InputSchema []byte    `json:"-"`
	Endpoint    string    `json:"-"`
	AuthConfig  string    `json:"-"`
	HasAuth     bool      `json:"has_auth"`
	Sensitive   bool      `json:"sensitive"`
	Published   bool      `json:"published"`
	CreatedAt   time.Time `json:"created_at"`

	authCiphertext []byte
}

func (r *Registry) List(_ context.Context, workspaceID string) []Version {
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := make([]Version, 0, len(r.versions))
	for _, version := range r.versions {
		if version.WorkspaceID != workspaceID {
			continue
		}
		version.InputSchema = append([]byte(nil), version.InputSchema...)
		version.AuthConfig = ""
		version.authCiphertext = nil
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID < versions[j].ID })
	return versions
}

type Registry struct {
	mu             sync.RWMutex
	versions       map[string]Version
	toolNameOwners map[string]map[string]string
	aead           cipher.AEAD
}

func NewRegistry(credentialKeys ...[]byte) *Registry {
	key := make([]byte, 32)
	if len(credentialKeys) > 0 && len(credentialKeys[0]) > 0 {
		digest := sha256.Sum256(credentialKeys[0])
		copy(key, digest[:])
	} else if _, err := io.ReadFull(rand.Reader, key); err != nil {
		panic(fmt.Errorf("generate credential key: %w", err))
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
		versions: make(map[string]Version), toolNameOwners: make(map[string]map[string]string), aead: aead,
	}
}

func (r *Registry) Register(_ context.Context, version Version) error {
	if strings.TrimSpace(version.ID) == "" || strings.TrimSpace(version.WorkspaceID) == "" || strings.TrimSpace(version.Name) == "" {
		return fmt.Errorf("id, workspace and name are required")
	}
	if strings.TrimSpace(version.ToolID) == "" {
		version.ToolID = version.ID
	}
	var schema map[string]any
	if err := json.Unmarshal(version.InputSchema, &schema); err != nil || schema["type"] != "object" {
		return fmt.Errorf("input schema must be a JSON object schema")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.versions[version.ID]; exists {
		return fmt.Errorf("tool version %s already exists", version.ID)
	}
	workspaceNames := r.toolNameOwners[version.WorkspaceID]
	if workspaceNames == nil {
		workspaceNames = make(map[string]string)
		r.toolNameOwners[version.WorkspaceID] = workspaceNames
	}
	nameKey := strings.ToLower(strings.TrimSpace(version.Name))
	if owner, exists := workspaceNames[nameKey]; exists && owner != version.ToolID {
		return fmt.Errorf("tool name %q already exists in workspace", version.Name)
	}
	ciphertext, err := r.encrypt(version.AuthConfig)
	if err != nil {
		return fmt.Errorf("encrypt tool credentials: %w", err)
	}
	version.HasAuth = len(ciphertext) > 0
	version.AuthConfig = ""
	version.authCiphertext = ciphertext
	version.InputSchema = append([]byte(nil), version.InputSchema...)
	r.versions[version.ID] = version
	workspaceNames[nameKey] = version.ToolID
	return nil
}

func (r *Registry) Resolve(_ context.Context, workspaceID, versionID string) (Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	version, ok := r.versions[versionID]
	if !ok || version.WorkspaceID != workspaceID {
		return Version{}, fmt.Errorf("tool version %s not found", versionID)
	}
	if !version.Published {
		return Version{}, fmt.Errorf("tool version %s is not published", versionID)
	}
	version.InputSchema = append([]byte(nil), version.InputSchema...)
	plaintext, err := r.decrypt(version.authCiphertext)
	if err != nil {
		return Version{}, fmt.Errorf("decrypt tool credentials: %w", err)
	}
	version.AuthConfig = plaintext
	version.authCiphertext = nil
	return version, nil
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
		return "", fmt.Errorf("invalid credential ciphertext")
	}
	plaintext, err := r.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
