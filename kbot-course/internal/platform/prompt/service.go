// Package prompt manages immutable templates, environment promotions and a
// compact rollout state used by the course control plane.
package prompt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"
)

type Prompt struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Version struct {
	ID                    string         `json:"id"`
	PromptID              string         `json:"prompt_id"`
	Version               int            `json:"version"`
	WorkspaceID           string         `json:"workspace_id"`
	Name                  string         `json:"name"`
	Category              string         `json:"category"`
	Template              string         `json:"template"`
	VariablesSchema       string         `json:"variables_schema"`
	ModelProfileVersionID string         `json:"model_profile_version_id,omitempty"`
	GenerationConfig      map[string]any `json:"generation_config"`
	Hash                  string         `json:"hash"`
	TokenEstimate         int            `json:"token_estimate"`
	CreatedAt             time.Time      `json:"created_at"`
}

type rollout struct {
	BaselineID, CandidateID string
	Traffic                 int
}

type Service struct {
	mu         sync.RWMutex
	prompts    map[string]Prompt
	versions   map[string]Version
	promotions map[string]string
	rollouts   map[string]rollout
	sequence   atomic.Uint64
}

func NewService() *Service {
	return &Service{prompts: make(map[string]Prompt), versions: make(map[string]Version), promotions: make(map[string]string), rollouts: make(map[string]rollout)}
}

func (s *Service) Publish(_ context.Context, version Version) error {
	if strings.TrimSpace(version.ID) == "" || strings.TrimSpace(version.WorkspaceID) == "" || strings.TrimSpace(version.Template) == "" {
		return fmt.Errorf("id, workspace and template are required")
	}
	if _, err := template.New(version.Name).Option("missingkey=error").Parse(version.Template); err != nil {
		return fmt.Errorf("parse prompt: %w", err)
	}
	if version.PromptID == "" {
		version.PromptID = version.ID
	}
	if version.Version <= 0 {
		version.Version = 1
	}
	if version.CreatedAt.IsZero() {
		version.CreatedAt = time.Now().UTC()
	}
	version.Hash = fmt.Sprintf("%x", sha256.Sum256([]byte(version.Template)))
	version.TokenEstimate = max(1, len([]rune(version.Template))/4)
	version.GenerationConfig = cloneMap(version.GenerationConfig)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.versions[version.ID]; exists {
		return fmt.Errorf("prompt version %s already exists", version.ID)
	}
	s.versions[version.ID] = version
	if _, exists := s.prompts[version.PromptID]; !exists {
		now := version.CreatedAt
		s.prompts[version.PromptID] = Prompt{ID: version.PromptID, WorkspaceID: version.WorkspaceID, Name: version.Name, Category: version.Category, CreatedAt: now, UpdatedAt: now}
	}
	if s.promotions[promotionKey(version.PromptID, "dev")] == "" {
		s.promotions[promotionKey(version.PromptID, "dev")] = version.ID
	}
	return nil
}

func (s *Service) Create(ctx context.Context, workspaceID, name, category, body, variables, modelVersionID string, generation map[string]any) (Prompt, Version, error) {
	id := fmt.Sprintf("prompt-%d", s.sequence.Add(1))
	version := Version{ID: fmt.Sprintf("prompt-version-%d", s.sequence.Add(1)), PromptID: id, Version: 1, WorkspaceID: workspaceID, Name: name, Category: category, Template: body, VariablesSchema: variables, ModelProfileVersionID: modelVersionID, GenerationConfig: generation, CreatedAt: time.Now().UTC()}
	if err := s.Publish(ctx, version); err != nil {
		return Prompt{}, Version{}, err
	}
	s.mu.RLock()
	prompt := s.prompts[id]
	version = s.versions[version.ID]
	s.mu.RUnlock()
	return prompt, cloneVersion(version), nil
}

func (s *Service) CreateVersion(ctx context.Context, workspaceID, promptID, body, variables, modelVersionID string, generation map[string]any) (Version, error) {
	s.mu.RLock()
	prompt, ok := s.prompts[promptID]
	count := 0
	for _, item := range s.versions {
		if item.PromptID == promptID {
			count++
		}
	}
	s.mu.RUnlock()
	if !ok || prompt.WorkspaceID != workspaceID {
		return Version{}, fmt.Errorf("prompt %s not found", promptID)
	}
	version := Version{ID: fmt.Sprintf("prompt-version-%d", s.sequence.Add(1)), PromptID: promptID, Version: count + 1, WorkspaceID: workspaceID, Name: prompt.Name, Category: prompt.Category, Template: body, VariablesSchema: variables, ModelProfileVersionID: modelVersionID, GenerationConfig: generation, CreatedAt: time.Now().UTC()}
	if err := s.Publish(ctx, version); err != nil {
		return Version{}, err
	}
	s.mu.RLock()
	version = s.versions[version.ID]
	s.mu.RUnlock()
	return cloneVersion(version), nil
}

func (s *Service) Render(_ context.Context, workspaceID, versionID string, variables map[string]string) (string, error) {
	converted := make(map[string]any, len(variables))
	for key, value := range variables {
		converted[key] = value
	}
	return s.render(workspaceID, versionID, converted)
}

func (s *Service) RenderEnvironment(workspaceID, promptID, environment string, variables map[string]any) (string, error) {
	if environment == "" {
		environment = "dev"
	}
	s.mu.RLock()
	versionID := s.promotions[promotionKey(promptID, environment)]
	s.mu.RUnlock()
	if versionID == "" {
		return "", fmt.Errorf("prompt %s has no version in %s", promptID, environment)
	}
	return s.render(workspaceID, versionID, variables)
}

func (s *Service) render(workspaceID, versionID string, variables any) (string, error) {
	s.mu.RLock()
	version, ok := s.versions[versionID]
	s.mu.RUnlock()
	if !ok || version.WorkspaceID != workspaceID {
		return "", fmt.Errorf("prompt version %s not found", versionID)
	}
	parsed, err := template.New(version.Name).Option("missingkey=error").Parse(version.Template)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := parsed.Execute(&output, variables); err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}
	return output.String(), nil
}

func (s *Service) List(_ context.Context, workspaceID string) []Version {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Version, 0)
	for _, version := range s.versions {
		if version.WorkspaceID == workspaceID {
			result = append(result, cloneVersion(version))
		}
	}
	return result
}

func (s *Service) ListPrompts(workspaceID string) []Prompt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Prompt, 0)
	for _, item := range s.prompts {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) ListVersions(workspaceID, promptID string) ([]Version, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prompt, ok := s.prompts[promptID]
	if !ok || prompt.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("prompt %s not found", promptID)
	}
	result := make([]Version, 0)
	for _, item := range s.versions {
		if item.PromptID == promptID {
			result = append(result, cloneVersion(item))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version > result[j].Version })
	return result, nil
}

func (s *Service) Promote(workspaceID, promptID, environment, versionID string) error {
	if environment != "dev" && environment != "staging" && environment != "prod" {
		return fmt.Errorf("environment must be dev, staging or prod")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt, ok := s.prompts[promptID]
	version, versionOK := s.versions[versionID]
	if !ok || prompt.WorkspaceID != workspaceID || !versionOK || version.PromptID != promptID {
		return fmt.Errorf("prompt or version not found")
	}
	s.promotions[promotionKey(promptID, environment)] = versionID
	return nil
}

func (s *Service) StartRollout(workspaceID, promptID, environment, candidateID string, traffic int) error {
	if traffic <= 0 || traffic >= 100 {
		return fmt.Errorf("traffic percent must be within 1..99")
	}
	if err := s.PromoteCheck(workspaceID, promptID, candidateID); err != nil {
		return err
	}
	s.mu.Lock()
	s.rollouts[promotionKey(promptID, environment)] = rollout{BaselineID: s.promotions[promotionKey(promptID, environment)], CandidateID: candidateID, Traffic: traffic}
	s.mu.Unlock()
	return nil
}

func (s *Service) UpdateRollout(promptID, environment string, traffic int) error {
	if traffic <= 0 || traffic >= 100 {
		return fmt.Errorf("traffic percent must be within 1..99")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := promotionKey(promptID, environment)
	item, ok := s.rollouts[key]
	if !ok {
		return fmt.Errorf("rollout not found")
	}
	item.Traffic = traffic
	s.rollouts[key] = item
	return nil
}

func (s *Service) CompleteRollout(promptID, environment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := promotionKey(promptID, environment)
	item, ok := s.rollouts[key]
	if !ok {
		return fmt.Errorf("rollout not found")
	}
	s.promotions[key] = item.CandidateID
	delete(s.rollouts, key)
	return nil
}

func (s *Service) RollbackRollout(promptID, environment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := promotionKey(promptID, environment)
	if _, ok := s.rollouts[key]; !ok {
		return fmt.Errorf("rollout not found")
	}
	delete(s.rollouts, key)
	return nil
}

func (s *Service) PromoteCheck(workspaceID, promptID, versionID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prompt, ok := s.prompts[promptID]
	version, versionOK := s.versions[versionID]
	if !ok || prompt.WorkspaceID != workspaceID || !versionOK || version.PromptID != promptID {
		return fmt.Errorf("prompt or version not found")
	}
	return nil
}

func promotionKey(promptID, environment string) string { return promptID + ":" + environment }
func cloneMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
func cloneVersion(version Version) Version {
	version.GenerationConfig = cloneMap(version.GenerationConfig)
	return version
}
