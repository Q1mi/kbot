package audit

import (
	"context"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
)

// MemoryStore 是 Audit 的内存存储。
type MemoryStore struct {
	mu            sync.RWMutex
	logs          []*domain.AuditLog
	skillTriggers []SkillTriggerRecord
}

type SkillTriggerRecord struct {
	WorkspaceID, ConversationID, SkillVersionID, SkillName, Source string
	TriggeredAt                                                    time.Time
}

// NewMemoryStore 创建内存审计存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// BatchInsert 批量插入。
func (s *MemoryStore) BatchInsert(_ context.Context, logs []*domain.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, logs...)
	return nil
}

// Query 按 conversation_id / actor 过滤，返回最新的 Limit 条。
func (s *MemoryStore) Query(_ context.Context, f QueryFilter) ([]*domain.AuditLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*domain.AuditLog
	for i := len(s.logs) - 1; i >= 0; i-- { // 倒序，最新优先
		l := s.logs[i]
		if f.WorkspaceID != "" && l.WorkspaceID != f.WorkspaceID {
			continue
		}
		if f.ConversationID != "" && !(l.ResourceType == "conversation" && l.ResourceID == f.ConversationID) {
			continue
		}
		if f.Actor != "" && l.Actor != f.Actor {
			continue
		}
		if f.ResourceType != "" && l.ResourceType != f.ResourceType {
			continue
		}
		out = append(out, l)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryStore) InsertSkillTrigger(
	_ context.Context, workspaceID, conversationID, skillVersionID, skillName, source string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skillTriggers = append(s.skillTriggers, SkillTriggerRecord{
		WorkspaceID: workspaceID, ConversationID: conversationID, SkillVersionID: skillVersionID,
		SkillName: skillName, Source: source, TriggeredAt: time.Now(),
	})
	return nil
}
