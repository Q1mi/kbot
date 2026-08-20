package skill

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Version struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Package     Package `json:"package"`
	Status      string  `json:"status"`
}

type Service struct {
	mu       sync.RWMutex
	versions map[string]Version
}

func NewService() *Service { return &Service{versions: make(map[string]Version)} }

func (s *Service) Publish(_ context.Context, id, workspaceID string, raw []byte) (*Version, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("skill version id and workspace are required")
	}
	pkg, err := ParseSkillMD(raw)
	if err != nil {
		return nil, err
	}
	version := Version{ID: id, WorkspaceID: workspaceID, Package: pkg, Status: "published"}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.versions[id]; exists {
		return nil, fmt.Errorf("skill version %s already exists", id)
	}
	s.versions[id] = version
	return &version, nil
}

func (s *Service) Resolve(_ context.Context, workspaceID, versionID string) (Version, error) {
	s.mu.RLock()
	version, ok := s.versions[versionID]
	s.mu.RUnlock()
	if !ok || version.WorkspaceID != workspaceID || version.Status != "published" {
		return Version{}, fmt.Errorf("published skill version %s not found", versionID)
	}
	return version, nil
}

func (s *Service) List(_ context.Context, workspaceID string) []Version {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Version, 0, len(s.versions))
	for _, version := range s.versions {
		if version.WorkspaceID == workspaceID {
			result = append(result, version)
		}
	}
	return result
}
