// Package agent 管理 Agent 版本、环境发布和会话固定。
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type Service struct {
	mu            sync.RWMutex
	agents        map[string]Agent
	versions      map[string]domain.AgentVersion
	snapshots     map[string]engine.AgentSnapshot
	promotions    map[string]string
	conversations map[string]domain.Conversation
	messages      map[string][]domain.Message
	postgres      *postgresStore
	sequence      atomic.Uint64
}

type Agent struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Template    string    `json:"template"`
	CreatedAt   time.Time `json:"created_at"`
}

func NewService() *Service {
	return &Service{
		agents: make(map[string]Agent), versions: make(map[string]domain.AgentVersion),
		snapshots: make(map[string]engine.AgentSnapshot), promotions: make(map[string]string),
		conversations: make(map[string]domain.Conversation),
		messages:      make(map[string][]domain.Message),
	}
}

func (s *Service) CreateAgent(ctx context.Context, workspaceID, name, template string) (*Agent, error) {
	if s.postgres != nil {
		return s.postgres.createAgent(ctx, workspaceID, name, template)
	}
	if workspaceID == "" || name == "" {
		return nil, fmt.Errorf("workspace and agent name are required")
	}
	agent := Agent{ID: fmt.Sprintf("agent-%d", s.sequence.Add(1)), WorkspaceID: workspaceID, Name: name, Template: template, CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.agents[agent.ID] = agent
	s.mu.Unlock()
	return &agent, nil
}

func (s *Service) ListAgents(ctx context.Context, workspaceID string) []Agent {
	if s.postgres != nil {
		result, _ := s.postgres.listAgents(ctx, workspaceID)
		return result
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Agent, 0, len(s.agents))
	for _, item := range s.agents {
		if item.WorkspaceID == workspaceID {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) GetAgent(ctx context.Context, workspaceID, agentID string) (Agent, error) {
	if s.postgres != nil {
		return s.postgres.getAgent(ctx, workspaceID, agentID)
	}
	s.mu.RLock()
	item, ok := s.agents[agentID]
	s.mu.RUnlock()
	if !ok || item.WorkspaceID != workspaceID {
		return Agent{}, fmt.Errorf("agent %s not found", agentID)
	}
	return item, nil
}

func (s *Service) Publish(ctx context.Context, version domain.AgentVersion, snapshot engine.AgentSnapshot) error {
	if version.ID == "" || version.AgentID == "" || version.WorkspaceID == "" {
		return fmt.Errorf("version id, agent and workspace are required")
	}
	if snapshot.ID != version.ID || snapshot.AgentID != version.AgentID || snapshot.WorkspaceID != version.WorkspaceID {
		return fmt.Errorf("snapshot identity must match agent version")
	}
	if s.postgres != nil {
		return s.postgres.publish(ctx, version, snapshot)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.versions[version.ID]; exists {
		return fmt.Errorf("agent version %s already exists", version.ID)
	}
	s.versions[version.ID] = version
	s.snapshots[version.ID] = cloneSnapshot(snapshot)
	return nil
}

func (s *Service) Promote(ctx context.Context, workspaceID, agentID, environment, versionID string) error {
	if s.postgres != nil {
		return s.postgres.promote(ctx, workspaceID, agentID, environment, versionID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok || version.WorkspaceID != workspaceID || version.AgentID != agentID {
		return fmt.Errorf("agent version %s not found", versionID)
	}
	s.promotions[promotionKey(workspaceID, agentID, environment)] = versionID
	return nil
}

func (s *Service) CreateConversation(ctx context.Context, workspaceID, agentID, environment, userID string) (*domain.Conversation, error) {
	if s.postgres != nil {
		return s.postgres.createConversation(ctx, workspaceID, agentID, environment, userID)
	}
	s.mu.RLock()
	versionID, ok := s.promotions[promotionKey(workspaceID, agentID, environment)]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent %s has no version promoted to %s", agentID, environment)
	}
	conversation := domain.Conversation{ID: fmt.Sprintf("conversation-%d", s.sequence.Add(1)), WorkspaceID: workspaceID, AgentID: agentID, AgentVersionID: versionID, UserID: userID, CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.conversations[conversation.ID] = conversation
	s.mu.Unlock()
	return &conversation, nil
}

// CreateConversationForVersion 为离线评测固定目标版本，避免环境指针在用例执行期间漂移。
func (s *Service) CreateConversationForVersion(ctx context.Context, workspaceID, agentID, versionID, userID string) (*domain.Conversation, error) {
	if s.postgres != nil {
		return s.postgres.createConversationForVersion(ctx, workspaceID, agentID, versionID, userID)
	}
	s.mu.RLock()
	version, ok := s.versions[versionID]
	s.mu.RUnlock()
	if !ok || version.WorkspaceID != workspaceID || version.AgentID != agentID {
		return nil, fmt.Errorf("agent version %s not found", versionID)
	}
	conversation := domain.Conversation{ID: fmt.Sprintf("conversation-%d", s.sequence.Add(1)), WorkspaceID: workspaceID, AgentID: agentID, AgentVersionID: versionID, UserID: userID, CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.conversations[conversation.ID] = conversation
	s.mu.Unlock()
	return &conversation, nil
}

func (s *Service) Snapshot(ctx context.Context, workspaceID, versionID string) (*engine.AgentSnapshot, error) {
	if s.postgres != nil {
		return s.postgres.snapshot(ctx, workspaceID, versionID)
	}
	s.mu.RLock()
	snapshot, ok := s.snapshots[versionID]
	s.mu.RUnlock()
	if !ok || snapshot.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("agent snapshot %s not found", versionID)
	}
	copy := cloneSnapshot(snapshot)
	return &copy, nil
}

func (s *Service) LoadConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	if s.postgres != nil {
		return s.postgres.loadConversation(ctx, conversationID)
	}
	s.mu.RLock()
	conversation, ok := s.conversations[conversationID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("conversation %s not found", conversationID)
	}
	return &conversation, nil
}

func (s *Service) ResolveConversation(ctx context.Context, workspaceID, userID, agentID, environment, conversationID string) (*domain.Conversation, error) {
	if conversationID == "" {
		if environment == "" {
			environment = "dev"
		}
		return s.CreateConversation(ctx, workspaceID, agentID, environment, userID)
	}
	conversation, err := s.LoadConversation(ctx, conversationID)
	if err != nil || conversation.WorkspaceID != workspaceID || conversation.UserID != userID || conversation.AgentID != agentID {
		return nil, fmt.Errorf("conversation %s not found", conversationID)
	}
	return conversation, nil
}

func (s *Service) GetAgentSnapshotByVersion(ctx context.Context, versionID string) (*engine.AgentSnapshot, error) {
	if s.postgres != nil {
		return s.postgres.snapshot(ctx, "", versionID)
	}
	s.mu.RLock()
	snapshot, ok := s.snapshots[versionID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent snapshot %s not found", versionID)
	}
	copy := cloneSnapshot(snapshot)
	return &copy, nil
}

func (s *Service) ListVersions(ctx context.Context, workspaceID, agentID string) []domain.AgentVersion {
	if s.postgres != nil {
		result, _ := s.postgres.listVersions(ctx, workspaceID, agentID)
		return result
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.AgentVersion, 0)
	for _, version := range s.versions {
		if version.WorkspaceID == workspaceID && version.AgentID == agentID {
			result = append(result, version)
		}
	}
	return result
}

func (s *Service) ListConversations(ctx context.Context, workspaceID, agentID string) []domain.Conversation {
	if s.postgres != nil {
		result, _ := s.postgres.listConversations(ctx, workspaceID, agentID)
		return result
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.Conversation, 0)
	for _, conversation := range s.conversations {
		if conversation.WorkspaceID == workspaceID && (agentID == "" || conversation.AgentID == agentID) {
			result = append(result, conversation)
		}
	}
	return result
}

func (s *Service) ListMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error) {
	if s.postgres != nil {
		return s.postgres.listMessages(ctx, workspaceID, conversationID)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	conversation, ok := s.conversations[conversationID]
	if !ok || conversation.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("conversation %s not found", conversationID)
	}
	return append([]domain.Message(nil), s.messages[conversationID]...), nil
}

func (s *Service) AppendMessage(ctx context.Context, workspaceID, conversationID, role, content string) error {
	if role != "user" && role != "assistant" {
		return fmt.Errorf("message role %q is not supported", role)
	}
	if content == "" {
		return fmt.Errorf("message content is required")
	}
	if s.postgres != nil {
		return s.postgres.appendMessage(ctx, workspaceID, conversationID, role, content)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok || conversation.WorkspaceID != workspaceID {
		return fmt.Errorf("conversation %s not found", conversationID)
	}
	message := domain.Message{
		ID: fmt.Sprintf("message-%d", s.sequence.Add(1)), ConversationID: conversationID,
		Role: role, Content: content, CreatedAt: time.Now().UTC(),
	}
	s.messages[conversationID] = append(s.messages[conversationID], message)
	return nil
}

func (s *Service) ConversationDetail(ctx context.Context, workspaceID, userID, conversationID string) (*domain.Conversation, []domain.Message, error) {
	conversation, err := s.LoadConversation(ctx, conversationID)
	if err != nil || conversation.WorkspaceID != workspaceID || conversation.UserID != userID {
		return nil, nil, fmt.Errorf("conversation %s not found", conversationID)
	}
	messages, err := s.ListMessages(ctx, workspaceID, conversationID)
	return conversation, messages, err
}

func promotionKey(workspaceID, agentID, environment string) string {
	return workspaceID + ":" + agentID + ":" + environment
}

func cloneSnapshot(snapshot engine.AgentSnapshot) engine.AgentSnapshot {
	snapshot.ToolVersionIDs = append([]string(nil), snapshot.ToolVersionIDs...)
	snapshot.SkillVersionIDs = append([]string(nil), snapshot.SkillVersionIDs...)
	snapshot.KnowledgeVersionIDs = append([]string(nil), snapshot.KnowledgeVersionIDs...)
	return snapshot
}
