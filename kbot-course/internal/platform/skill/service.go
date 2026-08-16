package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Skill struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
}

type Version struct {
	ID          string    `json:"id"`
	SkillID     string    `json:"skill_id"`
	Version     int       `json:"version"`
	WorkspaceID string    `json:"workspace_id"`
	Package     Package   `json:"package"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	Raw         []byte    `json:"-"`
}

func (v Version) MarshalJSON() ([]byte, error) {
	metadata, err := json.Marshal(v.Package)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": v.ID, "skill_id": v.SkillID, "version": v.Version, "workspace_id": v.WorkspaceID,
		"frontmatter_json": string(metadata), "body_md": v.Package.Instructions, "status": v.Status, "created_at": v.CreatedAt,
	})
}

type Service struct {
	mu       sync.RWMutex
	versions map[string]Version
	skills   map[string]Skill
	sequence atomic.Uint64
}

func NewService() *Service {
	return &Service{versions: make(map[string]Version), skills: make(map[string]Skill)}
}

func (s *Service) Publish(_ context.Context, id, workspaceID string, raw []byte) (*Version, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("skill version id and workspace are required")
	}
	pkg, err := ParseSkillMD(raw)
	if err != nil {
		return nil, err
	}
	version := Version{ID: id, SkillID: id, Version: 1, WorkspaceID: workspaceID, Package: pkg, Status: "published", CreatedAt: time.Now().UTC(), Raw: append([]byte(nil), raw...)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.versions[id]; exists {
		return nil, fmt.Errorf("skill version %s already exists", id)
	}
	s.versions[id] = version
	s.skills[id] = Skill{ID: id, WorkspaceID: workspaceID, Name: pkg.Name, CreatedAt: version.CreatedAt}
	copy := cloneVersion(version)
	return &copy, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, category string, raw []byte) (Skill, Version, error) {
	id := fmt.Sprintf("skill-%d", s.sequence.Add(1))
	version, err := s.Publish(ctx, id, workspaceID, raw)
	if err != nil {
		return Skill{}, Version{}, err
	}
	s.mu.Lock()
	item := s.skills[id]
	item.Category = category
	s.skills[id] = item
	s.mu.Unlock()
	return item, *version, nil
}

func (s *Service) CreateVersion(_ context.Context, workspaceID, skillID string, raw []byte) (Version, error) {
	pkg, err := ParseSkillMD(raw)
	if err != nil {
		return Version{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.skills[skillID]
	if !ok || item.WorkspaceID != workspaceID {
		return Version{}, fmt.Errorf("skill %s not found", skillID)
	}
	number := 1
	for _, version := range s.versions {
		if version.SkillID == skillID && version.Version >= number {
			number = version.Version + 1
		}
	}
	version := Version{ID: fmt.Sprintf("skill-version-%d", s.sequence.Add(1)), SkillID: skillID, Version: number, WorkspaceID: workspaceID, Package: pkg, Status: "draft", CreatedAt: time.Now().UTC(), Raw: append([]byte(nil), raw...)}
	s.versions[version.ID] = version
	return cloneVersion(version), nil
}

func (s *Service) PublishVersion(workspaceID, skillID, versionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok || version.WorkspaceID != workspaceID || version.SkillID != skillID {
		return fmt.Errorf("skill version not found")
	}
	version.Status = "published"
	s.versions[versionID] = version
	return nil
}

func (s *Service) Resolve(_ context.Context, workspaceID, versionID string) (Version, error) {
	s.mu.RLock()
	version, ok := s.versions[versionID]
	s.mu.RUnlock()
	if !ok || version.WorkspaceID != workspaceID || version.Status != "published" {
		return Version{}, fmt.Errorf("published skill version %s not found", versionID)
	}
	return cloneVersion(version), nil
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

func (s *Service) ListSkills(workspaceID string) []Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Skill, 0)
	for _, item := range s.skills {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) ListVersions(workspaceID, skillID string) ([]Version, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.skills[skillID]
	if !ok || item.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("skill %s not found", skillID)
	}
	result := make([]Version, 0)
	for _, version := range s.versions {
		if version.SkillID == skillID {
			result = append(result, cloneVersion(version))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func cloneVersion(version Version) Version {
	version.Raw = append([]byte(nil), version.Raw...)
	version.Package.AllowedTools = append([]string(nil), version.Package.AllowedTools...)
	version.Package.AllowedKBs = append([]string(nil), version.Package.AllowedKBs...)
	return version
}
