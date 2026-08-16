package platform

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/iam"
)

// MemoryIAMStore IAM内存存储
type MemoryIAMStore struct {
	mu         sync.RWMutex
	users      map[string]*domain.User
	workspaces map[string]*domain.Workspace
	members    map[string]map[string]*domain.WorkspaceMember
}

// NewMemoryIAMStore 创建IAM内存存储
func NewMemoryIAMStore() *MemoryIAMStore {
	return &MemoryIAMStore{
		users:      make(map[string]*domain.User),
		workspaces: make(map[string]*domain.Workspace),
		members:    make(map[string]map[string]*domain.WorkspaceMember),
	}
}

// GetUserByEmail 根据邮箱获取用户
func (s *MemoryIAMStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

// CreateUser 创建用户
func (s *MemoryIAMStore) CreateUser(ctx context.Context, user *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copyUser := *user
	if copyUser.Role == "" {
		copyUser.Role = iamGlobalMember
	}
	s.users[user.ID] = &copyUser
	*user = copyUser
	return nil
}

const iamGlobalMember = "member"

func (s *MemoryIAMStore) SetUserRole(_ context.Context, userID, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	user.Role = role
	user.UpdatedAt = time.Now()
	return nil
}

// ListUsers 列出用户,按创建时间倒序 + limit/offset(对齐 pg ListUsers)。
func (s *MemoryIAMStore) ListUsers(ctx context.Context, limit, offset int32) ([]*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]*domain.User, 0, len(s.users))
	for _, u := range s.users {
		all = append(all, u)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return pageUsers(all, limit, offset), nil
}

// GetUserByID 根据ID获取用户
func (s *MemoryIAMStore) GetUserByID(ctx context.Context, userID string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[userID]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}
	return user, nil
}

// GetWorkspaceByID 根据ID获取工作空间
func (s *MemoryIAMStore) GetWorkspaceByID(ctx context.Context, workspaceID string) (*domain.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, exists := s.workspaces[workspaceID]
	if !exists {
		return nil, fmt.Errorf("workspace not found")
	}
	return workspace, nil
}

// CreateWorkspace 创建工作空间
func (s *MemoryIAMStore) CreateWorkspace(ctx context.Context, workspace *domain.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.workspaces[workspace.ID] = workspace
	return nil
}

func (s *MemoryIAMStore) CreateWorkspaceWithOwner(_ context.Context, workspace *domain.Workspace, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.users[userID] == nil {
		return fmt.Errorf("user not found")
	}
	s.workspaces[workspace.ID] = workspace
	s.members[workspace.ID] = map[string]*domain.WorkspaceMember{
		userID: {WorkspaceID: workspace.ID, UserID: userID, Role: "owner", CreatedAt: time.Now()},
	}
	return nil
}

// ListWorkspaces 列出工作空间,按创建时间倒序 + limit/offset(对齐 pg ListWorkspaces)。
func (s *MemoryIAMStore) ListWorkspaces(ctx context.Context, limit, offset int32) ([]*domain.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]*domain.Workspace, 0, len(s.workspaces))
	for _, ws := range s.workspaces {
		all = append(all, ws)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return pageWorkspaces(all, limit, offset), nil
}

func (s *MemoryIAMStore) ListUserWorkspaces(_ context.Context, userID string, limit, offset int32) ([]*domain.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*domain.Workspace, 0)
	for workspaceID, members := range s.members {
		if _, ok := members[userID]; ok {
			all = append(all, s.workspaces[workspaceID])
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return pageWorkspaces(all, limit, offset), nil
}

func (s *MemoryIAMStore) GetWorkspaceMember(_ context.Context, workspaceID, userID string) (*domain.WorkspaceMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	member, ok := s.members[workspaceID][userID]
	if !ok {
		return nil, fmt.Errorf("workspace member not found")
	}
	copyMember := *member
	if user := s.users[userID]; user != nil {
		copyMember.Email = user.Email
		copyMember.Name = user.Name
	}
	return &copyMember, nil
}

func (s *MemoryIAMStore) ListWorkspaceMembers(_ context.Context, workspaceID string) ([]*domain.WorkspaceMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.WorkspaceMember, 0, len(s.members[workspaceID]))
	for userID, member := range s.members[workspaceID] {
		copyMember := *member
		if user := s.users[userID]; user != nil {
			copyMember.Email = user.Email
			copyMember.Name = user.Name
		}
		out = append(out, &copyMember)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryIAMStore) UpsertWorkspaceMember(_ context.Context, member *domain.WorkspaceMember) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaces[member.WorkspaceID] == nil {
		return fmt.Errorf("workspace not found")
	}
	if s.users[member.UserID] == nil {
		return fmt.Errorf("user not found")
	}
	if s.members[member.WorkspaceID] == nil {
		s.members[member.WorkspaceID] = make(map[string]*domain.WorkspaceMember)
	}
	if existing := s.members[member.WorkspaceID][member.UserID]; existing != nil {
		existing.Role = member.Role
		member.CreatedAt = existing.CreatedAt
		return nil
	}
	if member.CreatedAt.IsZero() {
		member.CreatedAt = time.Now()
	}
	copyMember := *member
	s.members[member.WorkspaceID][member.UserID] = &copyMember
	return nil
}

func (s *MemoryIAMStore) DeleteWorkspaceMember(_ context.Context, workspaceID, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.members[workspaceID] == nil || s.members[workspaceID][userID] == nil {
		return false, nil
	}
	delete(s.members[workspaceID], userID)
	return true, nil
}

func (s *MemoryIAMStore) UpsertWorkspaceMemberGuarded(
	_ context.Context, member *domain.WorkspaceMember, allowOwnerManagement bool,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workspaces[member.WorkspaceID] == nil {
		return fmt.Errorf("workspace not found")
	}
	if s.users[member.UserID] == nil {
		return fmt.Errorf("user not found")
	}
	if s.members[member.WorkspaceID] == nil {
		s.members[member.WorkspaceID] = make(map[string]*domain.WorkspaceMember)
	}
	existing := s.members[member.WorkspaceID][member.UserID]
	if (member.Role == iam.WorkspaceRoleOwner || existing != nil && existing.Role == iam.WorkspaceRoleOwner) && !allowOwnerManagement {
		return iam.ErrForbidden
	}
	if existing != nil && existing.Role == iam.WorkspaceRoleOwner && member.Role != iam.WorkspaceRoleOwner {
		owners := 0
		for _, candidate := range s.members[member.WorkspaceID] {
			if candidate.Role == iam.WorkspaceRoleOwner {
				owners++
			}
		}
		if owners <= 1 {
			return iam.ErrLastOwner
		}
	}
	if existing != nil {
		existing.Role = member.Role
		member.CreatedAt = existing.CreatedAt
		return nil
	}
	if member.CreatedAt.IsZero() {
		member.CreatedAt = time.Now()
	}
	copyMember := *member
	s.members[member.WorkspaceID][member.UserID] = &copyMember
	return nil
}

func (s *MemoryIAMStore) DeleteWorkspaceMemberGuarded(
	_ context.Context, workspaceID, userID string, allowOwnerManagement bool,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.members[workspaceID][userID]
	if target == nil {
		return false, nil
	}
	if target.Role == iam.WorkspaceRoleOwner {
		if !allowOwnerManagement {
			return false, iam.ErrForbidden
		}
		owners := 0
		for _, candidate := range s.members[workspaceID] {
			if candidate.Role == iam.WorkspaceRoleOwner {
				owners++
			}
		}
		if owners <= 1 {
			return false, iam.ErrLastOwner
		}
	}
	delete(s.members[workspaceID], userID)
	return true, nil
}

// MemoryPromptStore Prompt内存存储（Apollo 风格：版本 immutable + env 指针 + 实验）。
type MemoryPromptStore struct {
	mu            sync.RWMutex
	prompts       map[string]*domain.Prompt
	versions      map[string]*domain.PromptVersion
	bindings      map[string]map[string]string        // promptID -> env -> versionID
	experiments   map[string]*domain.PromptExperiment // "promptID@env" -> active experiment
	rolloutEvents []*domain.PromptRolloutEvent
}

// NewMemoryPromptStore 创建Prompt内存存储
func NewMemoryPromptStore() *MemoryPromptStore {
	return &MemoryPromptStore{
		prompts:     make(map[string]*domain.Prompt),
		versions:    make(map[string]*domain.PromptVersion),
		bindings:    make(map[string]map[string]string),
		experiments: make(map[string]*domain.PromptExperiment),
	}
}

func (s *MemoryPromptStore) CreatePrompt(ctx context.Context, prompt *domain.Prompt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.ID] = prompt
	if s.bindings[prompt.ID] == nil {
		s.bindings[prompt.ID] = make(map[string]string)
	}
	return nil
}

func (s *MemoryPromptStore) DeletePrompt(ctx context.Context, promptID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.prompts, promptID)
	delete(s.bindings, promptID)
	for id, version := range s.versions {
		if version.PromptID == promptID {
			delete(s.versions, id)
		}
	}
	for key, experiment := range s.experiments {
		if experiment.PromptID == promptID {
			delete(s.experiments, key)
		}
	}
	return nil
}

func (s *MemoryPromptStore) GetPrompt(ctx context.Context, promptID string) (*domain.Prompt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prompts[promptID]
	if !ok {
		return nil, fmt.Errorf("prompt not found")
	}
	return p, nil
}

func (s *MemoryPromptStore) ListPrompts(ctx context.Context, workspaceID string) ([]*domain.Prompt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Prompt
	for _, p := range s.prompts {
		if workspaceID == "" || p.WorkspaceID == workspaceID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// CreatePromptVersion 仅 insert（immutable），不动 env 指针。
func (s *MemoryPromptStore) CreatePromptVersion(ctx context.Context, version *domain.PromptVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[version.ID] = version
	return nil
}

func (s *MemoryPromptStore) GetPromptVersion(ctx context.Context, versionID string) (*domain.PromptVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[versionID]
	if !ok {
		return nil, fmt.Errorf("prompt version not found")
	}
	return v, nil
}

func (s *MemoryPromptStore) GetPromptVersionByNumber(ctx context.Context, promptID string, number int) (*domain.PromptVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.versions {
		if v.PromptID == promptID && v.Version == number {
			return v, nil
		}
	}
	return nil, fmt.Errorf("prompt version v%d not found", number)
}

func (s *MemoryPromptStore) ListPromptVersions(ctx context.Context, promptID string) ([]*domain.PromptVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.PromptVersion
	for _, v := range s.versions {
		if v.PromptID == promptID {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// SetEnvBinding 改 env 指针（= "发布"/回滚）。
func (s *MemoryPromptStore) SetEnvBinding(ctx context.Context, promptID, env, versionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bindings[promptID] == nil {
		s.bindings[promptID] = make(map[string]string)
	}
	s.bindings[promptID][env] = versionID
	return nil
}

func (s *MemoryPromptStore) GetEnvBinding(ctx context.Context, promptID, env string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bindings[promptID]
	if !ok {
		return "", fmt.Errorf("prompt not found")
	}
	vid, ok := b[env]
	if !ok {
		return "", fmt.Errorf("no version bound for env %s", env)
	}
	return vid, nil
}

func (s *MemoryPromptStore) GetActiveExperiment(ctx context.Context, promptID, env string) (*domain.PromptExperiment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.experiments[promptID+"@"+env]
	if !ok || exp.Status != "active" {
		return nil, nil
	}
	cloned := *exp
	cloned.Variants = append([]domain.ExperimentVariant(nil), exp.Variants...)
	return &cloned, nil
}

func (s *MemoryPromptStore) UpsertExperiment(ctx context.Context, exp *domain.PromptExperiment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *exp
	cloned.Variants = append([]domain.ExperimentVariant(nil), exp.Variants...)
	s.experiments[exp.PromptID+"@"+exp.Env] = &cloned
	return nil
}

func (s *MemoryPromptStore) AppendRolloutEvent(_ context.Context, event *domain.PromptRolloutEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rolloutEvents = append(s.rolloutEvents, event)
	return nil
}

func (s *MemoryPromptStore) CompleteRollout(_ context.Context, exp *domain.PromptExperiment, event *domain.PromptRolloutEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.experiments[exp.PromptID+"@"+exp.Env]
	if !ok || current.Status != "active" {
		return fmt.Errorf("active rollout no longer exists")
	}
	if s.bindings[exp.PromptID] == nil {
		s.bindings[exp.PromptID] = make(map[string]string)
	}
	s.bindings[exp.PromptID][exp.Env] = exp.CandidateVersionID
	cloned := *exp
	cloned.Variants = append([]domain.ExperimentVariant(nil), exp.Variants...)
	s.experiments[exp.PromptID+"@"+exp.Env] = &cloned
	s.rolloutEvents = append(s.rolloutEvents, event)
	return nil
}

// MemoryAgentStore Agent内存存储
type MemoryAgentStore struct {
	mu                sync.RWMutex
	agents            map[string]*domain.Agent
	agentVersions     map[string]*domain.AgentVersion
	agentBindings     map[string]map[string]string // agentID -> env -> versionID
	conversations     map[string]*domain.Conversation
	messages          map[string][]*domain.Message // conversationID -> messages
	conversationTurns map[string]memoryConversationTurn
}

type memoryConversationTurn struct {
	token      string
	leaseUntil time.Time
	revision   int64
}

// NewMemoryAgentStore 创建Agent内存存储
func NewMemoryAgentStore() *MemoryAgentStore {
	return &MemoryAgentStore{
		agents:            make(map[string]*domain.Agent),
		agentVersions:     make(map[string]*domain.AgentVersion),
		agentBindings:     make(map[string]map[string]string),
		conversations:     make(map[string]*domain.Conversation),
		messages:          make(map[string][]*domain.Message),
		conversationTurns: make(map[string]memoryConversationTurn),
	}
}

// GetAgent 获取Agent
func (s *MemoryAgentStore) GetAgent(ctx context.Context, agentID string) (*domain.Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, exists := s.agents[agentID]
	if !exists {
		return nil, fmt.Errorf("agent not found")
	}
	return agent, nil
}

// ListAgents 列出工作空间下的 Agents,按创建时间升序(对齐 pg ListAgents)。
func (s *MemoryAgentStore) ListAgents(ctx context.Context, workspaceID string) ([]*domain.Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*domain.Agent, 0)
	for _, a := range s.agents {
		if a.WorkspaceID == workspaceID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// CreateAgent 创建Agent
func (s *MemoryAgentStore) CreateAgent(ctx context.Context, agent *domain.Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.agents[agent.ID] = agent
	if s.agentBindings[agent.ID] == nil {
		s.agentBindings[agent.ID] = make(map[string]string)
	}
	return nil
}

func (s *MemoryAgentStore) CreateAgentWithVersion(_ context.Context, agent *domain.Agent, version *domain.AgentVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[agent.ID] = agent
	s.agentVersions[version.ID] = version
	s.agentBindings[agent.ID] = map[string]string{"dev": version.ID}
	return nil
}

// CreateAgentVersion 创建Agent版本
func (s *MemoryAgentStore) CreateAgentVersion(ctx context.Context, version *domain.AgentVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.agentVersions[version.ID] = version
	// 自动设置为dev环境的当前版本
	if s.agentBindings[version.AgentID] == nil {
		s.agentBindings[version.AgentID] = make(map[string]string)
	}
	s.agentBindings[version.AgentID]["dev"] = version.ID
	return nil
}

// GetAgentVersion 按版本 ID 取（老对话按 pinned 版本解析快照用）。
func (s *MemoryAgentStore) GetAgentVersion(ctx context.Context, versionID string) (*domain.AgentVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.agentVersions[versionID]
	if !ok {
		return nil, fmt.Errorf("agent version not found")
	}
	return v, nil
}

func (s *MemoryAgentStore) ListAgentVersions(ctx context.Context, agentID string) ([]*domain.AgentVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.AgentVersion, 0)
	for _, version := range s.agentVersions {
		if version.AgentID == agentID {
			out = append(out, version)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// GetAgentCurrentVersion 获取Agent当前版本
func (s *MemoryAgentStore) GetAgentCurrentVersion(ctx context.Context, agentID, env string) (*domain.AgentVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	envBindings, exists := s.agentBindings[agentID]
	if !exists {
		return nil, fmt.Errorf("agent not found")
	}

	versionID, exists := envBindings[env]
	if !exists {
		return nil, fmt.Errorf("no version for env %s", env)
	}

	version, exists := s.agentVersions[versionID]
	if !exists {
		return nil, fmt.Errorf("agent version not found")
	}

	return version, nil
}

func (s *MemoryAgentStore) SetAgentEnvBinding(ctx context.Context, agentID, env, versionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.agentVersions[versionID]
	if !ok || version.AgentID != agentID {
		return fmt.Errorf("agent version not found")
	}
	if s.agentBindings[agentID] == nil {
		s.agentBindings[agentID] = make(map[string]string)
	}
	s.agentBindings[agentID][env] = versionID
	return nil
}

// CreateConversation 创建会话
func (s *MemoryAgentStore) CreateConversation(ctx context.Context, conversation *domain.Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.conversations[conversation.ID] = conversation
	s.messages[conversation.ID] = []*domain.Message{}
	return nil
}

func (s *MemoryAgentStore) ClaimConversationTurn(_ context.Context, conversationID string, resume bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return "", fmt.Errorf("conversation not found")
	}
	turn := s.conversationTurns[conversationID]
	now := time.Now()
	if turn.token != "" && turn.leaseUntil.After(now) {
		return "", fmt.Errorf("conversation turn is already running")
	}
	allowed := conversation.Status == "active"
	if resume {
		allowed = conversation.Status == "awaiting_approval" || conversation.Status == "active"
	}
	if !allowed && !(conversation.Status == "running" && turn.leaseUntil.Before(now)) {
		return "", fmt.Errorf("conversation is not ready for this turn")
	}
	turn.revision++
	turn.token = fmt.Sprintf("turn-%d-%d", now.UnixNano(), turn.revision)
	turn.leaseUntil = now.Add(2 * time.Minute)
	s.conversationTurns[conversationID] = turn
	conversation.Status = "running"
	conversation.UpdatedAt = now
	return turn.token, nil
}

func (s *MemoryAgentStore) RenewConversationTurn(_ context.Context, conversationID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn, ok := s.conversationTurns[conversationID]
	if !ok || turn.token != token || s.conversations[conversationID].Status != "running" {
		return fmt.Errorf("conversation turn lease is unavailable")
	}
	turn.leaseUntil = time.Now().Add(2 * time.Minute)
	s.conversationTurns[conversationID] = turn
	return nil
}

func (s *MemoryAgentStore) CommitConversationTurn(
	_ context.Context, conversationID, token string, messages []*domain.Message, nextStatus string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	turn, ok := s.conversationTurns[conversationID]
	conversation := s.conversations[conversationID]
	if !ok || conversation == nil || turn.token != token || conversation.Status != "running" {
		return fmt.Errorf("conversation turn lease is unavailable")
	}
	for _, message := range messages {
		if message == nil {
			continue
		}
		s.messages[conversationID] = append(s.messages[conversationID], message)
	}
	turn.token = ""
	turn.leaseUntil = time.Time{}
	s.conversationTurns[conversationID] = turn
	conversation.Status = nextStatus
	conversation.UpdatedAt = time.Now()
	return nil
}

func (s *MemoryAgentStore) ReleaseConversationTurn(_ context.Context, conversationID, token, nextStatus string) error {
	return s.CommitConversationTurn(context.Background(), conversationID, token, nil, nextStatus)
}

// UpdateConversationRuntimeConfig 更新会话实际使用的 Prompt 版本与渲染输入快照。
func (s *MemoryAgentStore) UpdateConversationRuntimeConfig(_ context.Context, conversationID, configJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return fmt.Errorf("conversation not found")
	}
	conversation.RuntimeConfigJSON = configJSON
	conversation.UpdatedAt = time.Now()
	return nil
}

// GetConversation 取会话。
func (s *MemoryAgentStore) GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.conversations[conversationID]
	if !ok {
		return nil, fmt.Errorf("conversation not found")
	}
	return c, nil
}

func (s *MemoryAgentStore) ListConversations(
	_ context.Context, workspaceID, userID, agentID string, limit, offset int32,
) ([]*domain.Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Conversation, 0)
	for _, conversation := range s.conversations {
		if conversation.WorkspaceID == workspaceID && conversation.UserID == userID &&
			(agentID == "" || conversation.AgentID == agentID) {
			out = append(out, conversation)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	lo, hi := sliceRange(len(out), limit, offset)
	return out[lo:hi], nil
}

// GetConversationMessages 获取会话消息
func (s *MemoryAgentStore) GetConversationMessages(ctx context.Context, conversationID string) ([]*domain.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages, exists := s.messages[conversationID]
	if !exists {
		return []*domain.Message{}, nil
	}
	return messages, nil
}

// CreateMessage 创建消息
func (s *MemoryAgentStore) CreateMessage(ctx context.Context, message *domain.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := s.messages[message.ConversationID]
	messages = append(messages, message)
	s.messages[message.ConversationID] = messages
	if conversation := s.conversations[message.ConversationID]; conversation != nil {
		conversation.UpdatedAt = time.Now()
	}
	return nil
}

// pageUsers / pageWorkspaces 把已排序切片按 limit/offset 截取(内存版分页,对齐 pg LIMIT/OFFSET)。
func pageUsers(all []*domain.User, limit, offset int32) []*domain.User {
	lo, hi := sliceRange(len(all), limit, offset)
	return all[lo:hi]
}

func pageWorkspaces(all []*domain.Workspace, limit, offset int32) []*domain.Workspace {
	lo, hi := sliceRange(len(all), limit, offset)
	return all[lo:hi]
}

// sliceRange 把 limit/offset 夹到 [0,n] 的合法区间;limit<=0 表示不限。
func sliceRange(n int, limit, offset int32) (int, int) {
	lo := int(offset)
	if lo < 0 {
		lo = 0
	}
	if lo > n {
		lo = n
	}
	hi := n
	if limit > 0 && lo+int(limit) < hi {
		hi = lo + int(limit)
	}
	return lo, hi
}
