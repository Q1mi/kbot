// Package platform 是 Agent 控制面。
package platform

import (
	"context"
	"fmt"
	"sync"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

// Platform 是课程前半段使用的内存控制面，便于聚焦运行链路。
type Platform struct {
	mu            sync.RWMutex
	conversations map[string]*domain.Conversation
	snapshots     map[string]*engine.AgentSnapshot
}

func New() *Platform {
	return &Platform{
		conversations: make(map[string]*domain.Conversation),
		snapshots:     make(map[string]*engine.AgentSnapshot),
	}
}

func (p *Platform) PutConversation(conversation *domain.Conversation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	copy := *conversation
	p.conversations[copy.ID] = &copy
}

func (p *Platform) PutSnapshot(snapshot *engine.AgentSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	copy := *snapshot
	copy.ToolVersionIDs = append([]string(nil), snapshot.ToolVersionIDs...)
	copy.SkillVersionIDs = append([]string(nil), snapshot.SkillVersionIDs...)
	p.snapshots[copy.ID] = &copy
}

func (p *Platform) LoadConversation(_ context.Context, id string) (*domain.Conversation, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	conversation, ok := p.conversations[id]
	if !ok {
		return nil, fmt.Errorf("conversation %s not found", id)
	}
	copy := *conversation
	return &copy, nil
}

func (p *Platform) GetAgentSnapshotByVersion(_ context.Context, id string) (*engine.AgentSnapshot, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	snapshot, ok := p.snapshots[id]
	if !ok {
		return nil, fmt.Errorf("agent snapshot %s not found", id)
	}
	copy := *snapshot
	copy.ToolVersionIDs = append([]string(nil), snapshot.ToolVersionIDs...)
	copy.SkillVersionIDs = append([]string(nil), snapshot.SkillVersionIDs...)
	return &copy, nil
}

var _ engine.Platform = (*Platform)(nil)
