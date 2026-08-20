// Package prompt 管理可审计、可固定引用的 Prompt 版本。
package prompt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"

	einoprompt "github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

type Version struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Template    string `json:"template"`
}

type compiledVersion struct {
	version Version
	tmpl    einoprompt.ChatTemplate
}

type Service struct {
	mu       sync.RWMutex
	versions map[string]compiledVersion
}

func NewService() *Service { return &Service{versions: make(map[string]compiledVersion)} }

func (s *Service) Publish(_ context.Context, version Version) error {
	if strings.TrimSpace(version.ID) == "" || strings.TrimSpace(version.WorkspaceID) == "" || strings.TrimSpace(version.Template) == "" {
		return fmt.Errorf("id, workspace and template are required")
	}
	// 发布时提前检查 Go template 语法；运行时交给 Eino ChatTemplate 格式化。
	if _, err := template.New(version.Name).Option("missingkey=error").Parse(version.Template); err != nil {
		return fmt.Errorf("parse prompt: %w", err)
	}
	compiled := compiledVersion{
		version: version,
		tmpl: einoprompt.FromMessages(schema.GoTemplate,
			schema.SystemMessage(version.Template),
		),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.versions[version.ID]; exists {
		return fmt.Errorf("prompt version %s already exists", version.ID)
	}
	s.versions[version.ID] = compiled
	return nil
}

func (s *Service) Render(ctx context.Context, workspaceID, versionID string, variables map[string]string) (string, error) {
	s.mu.RLock()
	compiled, ok := s.versions[versionID]
	s.mu.RUnlock()
	if !ok || compiled.version.WorkspaceID != workspaceID {
		return "", fmt.Errorf("prompt version %s not found", versionID)
	}
	values := make(map[string]any, len(variables))
	for key, value := range variables {
		values[key] = value
	}
	messages, err := compiled.tmpl.Format(ctx, values)
	if err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}
	if len(messages) != 1 {
		return "", fmt.Errorf("render prompt: expected one message, got %d", len(messages))
	}
	return messages[0].Content, nil
}

func (s *Service) List(_ context.Context, workspaceID string) []Version {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Version, 0, len(s.versions))
	for _, compiled := range s.versions {
		if compiled.version.WorkspaceID == workspaceID {
			result = append(result, compiled.version)
		}
	}
	return result
}
