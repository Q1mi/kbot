package platform

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Q1mi/kbot/internal/domain"
)

// MemoryToolStore Tool Registry 内存存储。
type MemoryToolStore struct {
	mu                sync.RWMutex
	tools             map[string]*domain.Tool
	versions          map[string]*domain.ToolVersion
	current           map[string]string // toolID -> 最新 versionID
	testRuns          map[string][]*domain.ToolTestRun
	invocations       map[string]*domain.ToolInvocation
	sandboxExecutions map[string]*domain.SandboxExecution
}

// NewMemoryToolStore 创建 Tool 内存存储。
func NewMemoryToolStore() *MemoryToolStore {
	return &MemoryToolStore{
		tools:             make(map[string]*domain.Tool),
		versions:          make(map[string]*domain.ToolVersion),
		current:           make(map[string]string),
		testRuns:          make(map[string][]*domain.ToolTestRun),
		invocations:       make(map[string]*domain.ToolInvocation),
		sandboxExecutions: make(map[string]*domain.SandboxExecution),
	}
}

func (s *MemoryToolStore) GetTool(ctx context.Context, toolID string) (*domain.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tools[toolID]
	if !ok {
		return nil, fmt.Errorf("tool not found")
	}
	return t, nil
}

func (s *MemoryToolStore) CreateTool(ctx context.Context, tool *domain.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tools {
		if existing.WorkspaceID == tool.WorkspaceID && existing.Name == tool.Name {
			return fmt.Errorf("tool name already exists in workspace")
		}
	}
	s.tools[tool.ID] = tool
	return nil
}

func (s *MemoryToolStore) CreateToolWithVersion(_ context.Context, tool *domain.Tool, version *domain.ToolVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tools {
		if existing.WorkspaceID == tool.WorkspaceID && existing.Name == tool.Name {
			return fmt.Errorf("tool name already exists in workspace")
		}
	}
	s.tools[tool.ID] = tool
	s.versions[version.ID] = version
	s.current[tool.ID] = version.ID
	return nil
}

func (s *MemoryToolStore) ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Tool
	for _, t := range s.tools {
		if workspaceID == "" || t.WorkspaceID == workspaceID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryToolStore) GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[versionID]
	if !ok {
		return nil, fmt.Errorf("tool version not found")
	}
	return v, nil
}

func (s *MemoryToolStore) CreateToolVersion(ctx context.Context, version *domain.ToolVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[version.ID] = version
	s.current[version.ToolID] = version.ID
	return nil
}

func (s *MemoryToolStore) GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vid, ok := s.current[toolID]
	if !ok {
		return nil, fmt.Errorf("tool has no version")
	}
	return s.versions[vid], nil
}

func (s *MemoryToolStore) GetToolLatestPublishedVersion(_ context.Context, toolID string) (*domain.ToolVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *domain.ToolVersion
	for _, v := range s.versions {
		if v.ToolID == toolID && v.Status == "published" && (latest == nil || v.Version > latest.Version) {
			latest = v
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("tool has no published version")
	}
	return latest, nil
}

func (s *MemoryToolStore) ListToolVersions(_ context.Context, toolID string) ([]*domain.ToolVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.ToolVersion
	for _, v := range s.versions {
		if v.ToolID == toolID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (s *MemoryToolStore) UpdateToolVersionStatus(ctx context.Context, versionID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.versions[versionID]
	if !ok {
		return fmt.Errorf("tool version not found")
	}
	v.Status = status
	return nil
}

func (s *MemoryToolStore) ListLegacyToolAuthVersions(_ context.Context) ([]*domain.ToolVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.ToolVersion
	for _, version := range s.versions {
		if len(version.AuthConfigEncrypted) == 0 && version.AuthConfig != "" && version.AuthConfig != "{}" {
			clone := *version
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (s *MemoryToolStore) EncryptToolVersionAuth(_ context.Context, versionID string, ciphertext []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok {
		return fmt.Errorf("tool version not found")
	}
	if len(version.AuthConfigEncrypted) == 0 {
		version.AuthConfig = "{}"
		version.AuthConfigEncrypted = append([]byte(nil), ciphertext...)
	}
	return nil
}

func (s *MemoryToolStore) CreateInvocation(_ context.Context, invocation *domain.ToolInvocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.invocations {
		if invocation.ToolCallID != "" && existing.ConversationID == invocation.ConversationID && existing.ToolCallID == invocation.ToolCallID {
			return fmt.Errorf("tool invocation already exists")
		}
	}
	copyInvocation := *invocation
	s.invocations[invocation.ID] = &copyInvocation
	return nil
}

func (s *MemoryToolStore) CompleteInvocation(_ context.Context, invocationID, result, status string, latencyMS int, errorMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invocation := s.invocations[invocationID]
	if invocation == nil || invocation.Status != "running" {
		return fmt.Errorf("tool invocation is not running")
	}
	invocation.Result = result
	invocation.Status = status
	invocation.LatencyMS = latencyMS
	invocation.Error = errorMessage
	return nil
}

func (s *MemoryToolStore) CreateSandboxExecution(_ context.Context, execution *domain.SandboxExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.sandboxExecutions {
		if execution.ExecutionID != "" && existing.ExecutionID == execution.ExecutionID {
			return fmt.Errorf("sandbox execution already exists")
		}
	}
	copyExecution := *execution
	s.sandboxExecutions[execution.ID] = &copyExecution
	return nil
}

func (s *MemoryToolStore) CreateTestRun(ctx context.Context, testRun *domain.ToolTestRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.testRuns[testRun.ToolID] = append(s.testRuns[testRun.ToolID], testRun)
	return nil
}

func (s *MemoryToolStore) GetToolLastSuccessfulTestRun(ctx context.Context, toolID string) (*domain.ToolTestRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := s.testRuns[toolID]
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == "success" {
			return runs[i], nil
		}
	}
	return nil, fmt.Errorf("no successful test run")
}

func (s *MemoryToolStore) GetToolLastSuccessfulTestRunForVersion(_ context.Context, versionID string) (*domain.ToolTestRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, runs := range s.testRuns {
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].ToolVersionID == versionID && runs[i].Status == "success" {
				return runs[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no successful test run for tool version")
}

// MemoryKBStore 知识库内存存储。
type MemoryKBStore struct {
	mu         sync.RWMutex
	kbs        map[string]*domain.KnowledgeBase
	documents  map[string]*domain.KbDocument
	ingestJobs map[string][]*domain.KbIngestJob       // kbID -> jobs
	connectors map[string][]*domain.ConnectorInstance // kbID -> connectors
}

// NewMemoryKBStore 创建 KB 内存存储。
func NewMemoryKBStore() *MemoryKBStore {
	return &MemoryKBStore{
		kbs:        make(map[string]*domain.KnowledgeBase),
		documents:  make(map[string]*domain.KbDocument),
		ingestJobs: make(map[string][]*domain.KbIngestJob),
		connectors: make(map[string][]*domain.ConnectorInstance),
	}
}

func (s *MemoryKBStore) CreateKB(ctx context.Context, kb *domain.KnowledgeBase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kbs[kb.ID] = kb
	return nil
}

func (s *MemoryKBStore) GetKB(ctx context.Context, kbID string) (*domain.KnowledgeBase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	kb, ok := s.kbs[kbID]
	if !ok {
		return nil, fmt.Errorf("kb not found")
	}
	return kb, nil
}

func (s *MemoryKBStore) ListKBs(ctx context.Context, workspaceID string) ([]*domain.KnowledgeBase, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.KnowledgeBase
	for _, kb := range s.kbs {
		if workspaceID == "" || kb.WorkspaceID == workspaceID {
			out = append(out, kb)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryKBStore) UpdateKBStatus(ctx context.Context, kbID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kb, ok := s.kbs[kbID]
	if !ok {
		return fmt.Errorf("kb not found")
	}
	kb.Status = status
	return nil
}

func (s *MemoryKBStore) UpsertDocument(ctx context.Context, doc *domain.KbDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents[doc.ID] = doc
	return nil
}

func (s *MemoryKBStore) GetDocument(ctx context.Context, docID string) (*domain.KbDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.documents[docID]
	if !ok {
		return nil, nil
	}
	return doc, nil
}

func (s *MemoryKBStore) ListDocuments(ctx context.Context, kbID string) ([]*domain.KbDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.KbDocument
	for _, doc := range s.documents {
		if doc.KbID == kbID {
			out = append(out, doc)
		}
	}
	return out, nil
}

func (s *MemoryKBStore) CreateIngestJob(ctx context.Context, job *domain.KbIngestJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ingestJobs[job.KbID] = append(s.ingestJobs[job.KbID], job)
	return nil
}

func (s *MemoryKBStore) UpdateIngestJob(ctx context.Context, job *domain.KbIngestJob) error {
	// job 为指针，CreateIngestJob 存的就是同一指针，原地更新即可。
	return nil
}

func (s *MemoryKBStore) ListIngestJobs(ctx context.Context, kbID string) ([]*domain.KbIngestJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ingestJobs[kbID], nil
}

func (s *MemoryKBStore) UpsertConnector(ctx context.Context, c *domain.ConnectorInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// upsert 语义:同 ID 覆盖,否则追加(与 PG 实现及方法名一致;原实现只 append 是疏漏)。
	for i, existing := range s.connectors[c.KbID] {
		if existing.ID == c.ID {
			s.connectors[c.KbID][i] = c
			return nil
		}
	}
	s.connectors[c.KbID] = append(s.connectors[c.KbID], c)
	return nil
}

func (s *MemoryKBStore) ListConnectors(ctx context.Context, kbID string) ([]*domain.ConnectorInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connectors[kbID], nil
}

// MemorySkillStore 技能内存存储。
type MemorySkillStore struct {
	mu       sync.RWMutex
	skills   map[string]*domain.Skill
	versions map[string]*domain.SkillVersion
	subs     map[string][]*domain.SkillSubscription // agentID -> subscriptions
}

// NewMemorySkillStore 创建技能内存存储。
func NewMemorySkillStore() *MemorySkillStore {
	return &MemorySkillStore{
		skills:   make(map[string]*domain.Skill),
		versions: make(map[string]*domain.SkillVersion),
		subs:     make(map[string][]*domain.SkillSubscription),
	}
}

func (s *MemorySkillStore) CreateSkill(ctx context.Context, sk *domain.Skill) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills[sk.ID] = sk
	return nil
}

func (s *MemorySkillStore) CreateSkillWithVersion(_ context.Context, sk *domain.Skill, version *domain.SkillVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills[sk.ID] = sk
	s.versions[version.ID] = version
	return nil
}

func (s *MemorySkillStore) GetSkill(ctx context.Context, skillID string) (*domain.Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sk, ok := s.skills[skillID]
	if !ok {
		return nil, fmt.Errorf("skill not found")
	}
	return sk, nil
}

func (s *MemorySkillStore) ListSkills(ctx context.Context, workspaceID string) ([]*domain.Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Skill
	for _, sk := range s.skills {
		if workspaceID == "" || sk.WorkspaceID == workspaceID {
			out = append(out, sk)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemorySkillStore) CreateSkillVersion(ctx context.Context, v *domain.SkillVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[v.ID] = v
	return nil
}

func (s *MemorySkillStore) GetSkillVersion(ctx context.Context, versionID string) (*domain.SkillVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[versionID]
	if !ok {
		return nil, fmt.Errorf("skill version not found")
	}
	return v, nil
}

func (s *MemorySkillStore) ListSkillVersions(ctx context.Context, skillID string) ([]*domain.SkillVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.SkillVersion
	for _, v := range s.versions {
		if v.SkillID == skillID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func (s *MemorySkillStore) UpdateSkillVersionStatus(ctx context.Context, versionID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.versions[versionID]
	if !ok {
		return fmt.Errorf("skill version not found")
	}
	v.Status = status
	return nil
}

func (s *MemorySkillStore) CreateSubscription(ctx context.Context, sub *domain.SkillSubscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs[sub.AgentID] = append(s.subs[sub.AgentID], sub)
	return nil
}

func (s *MemorySkillStore) ListSubscriptionsForAgent(ctx context.Context, agentID string) ([]*domain.SkillSubscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subs[agentID], nil
}
