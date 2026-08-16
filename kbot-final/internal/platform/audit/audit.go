// Package audit 是 Agent 的"飞行记录仪"（设计文档 §4.11 / 讲义 §15.6）。
//
// 对话级 Audit 要留住完整 prompt + tool calls + LLM response + skill triggers。
// 写入侧异步批量，绝不阻塞回答：队列满时丢弃并记录 metric，保护对话热路径延迟。
package audit

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/guard"
	"github.com/Q1mi/kbot/internal/util"
)

// Store 是 Audit 的持久化接口。
type Store interface {
	BatchInsert(ctx context.Context, logs []*domain.AuditLog) error
	Query(ctx context.Context, f QueryFilter) ([]*domain.AuditLog, error)
}

// SkillTriggerStore 持久化结构化 Skill 触发记录。独立接口让测试存储和旧实现可以渐进升级。
type SkillTriggerStore interface {
	InsertSkillTrigger(ctx context.Context, workspaceID, conversationID, skillVersionID, skillName, source string) error
}

// QueryFilter 审计检索条件（按 conversation_id / actor / resource_type 维度，§15.6）。
type QueryFilter struct {
	WorkspaceID    string
	ConversationID string
	Actor          string
	ResourceType   string
	Limit          int
}

// Writer 异步批量写入审计日志。
type Writer struct {
	in      chan *domain.AuditLog
	store   Store
	dropped int64
	quit    chan struct{}
	done    chan struct{}
	once    sync.Once
	stateMu sync.RWMutex
	closed  bool
}

// NewWriter 创建并启动异步写入器。
func NewWriter(store Store) *Writer {
	w := &Writer{
		in:    make(chan *domain.AuditLog, 1024),
		store: store,
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go w.loop()
	return w
}

// Write 非阻塞投递。队列满则丢弃并计数（绝不阻塞回答）。
func (w *Writer) Write(e *domain.AuditLog) {
	if e == nil {
		atomic.AddInt64(&w.dropped, 1)
		return
	}
	if e.ID == "" {
		e.ID = util.GenerateID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	if w.closed {
		atomic.AddInt64(&w.dropped, 1)
		return
	}
	select {
	case w.in <- e:
	default:
		atomic.AddInt64(&w.dropped, 1)
	}
}

// Dropped 返回未写入的日志数。
func (w *Writer) Dropped() int64 { return atomic.LoadInt64(&w.dropped) }

// Close 刷净剩余并停止。
func (w *Writer) Close() {
	w.once.Do(func() {
		w.stateMu.Lock()
		w.closed = true
		close(w.quit)
		w.stateMu.Unlock()
		<-w.done
	})
}

func (w *Writer) loop() {
	defer close(w.done)
	batch := make([]*domain.AuditLog, 0, 64)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := w.store.BatchInsert(ctx, batch)
		cancel()
		if err != nil {
			atomic.AddInt64(&w.dropped, int64(len(batch)))
			log.Printf("audit batch insert failed: count=%d error=%v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-w.in:
			batch = append(batch, e)
			if len(batch) >= 64 {
				flush()
			}
		case <-tick.C:
			flush()
		case <-w.quit:
			// 排空通道后最后刷一次。
			for {
				select {
				case e := <-w.in:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Service 封装 Audit 的查询与便捷写入。
type Service struct {
	writer *Writer
	store  Store
}

// NewService 创建 Audit 服务。
func NewService(store Store) *Service {
	return &Service{writer: NewWriter(store), store: store}
}

// Record 记录一条审计（便捷封装）。
func (s *Service) Record(ctx context.Context, actor, action, resourceType, resourceID string) {
	s.writer.Write(&domain.AuditLog{
		WorkspaceID: guard.WorkspaceKey(ctx),
		Actor:       actor, Action: action,
		ResourceType: resourceType, ResourceID: resourceID,
	})
}

// RecordWorkspace 记录控制面动作，由调用方显式提供已授权的工作空间。
func (s *Service) RecordWorkspace(workspaceID, actor, action, resourceType, resourceID string) {
	s.writer.Write(&domain.AuditLog{
		WorkspaceID: workspaceID, Actor: actor, Action: action,
		ResourceType: resourceType, ResourceID: resourceID,
	})
}

// RecordConversation 记录一条对话轨迹事件（conversation_id 入 ResourceID）。
func (s *Service) RecordConversation(ctx context.Context, conversationID, actor, action, detail string) {
	s.writer.Write(&domain.AuditLog{
		WorkspaceID: guard.WorkspaceKey(ctx),
		Actor:       actor, Action: action,
		ResourceType: "conversation", ResourceID: conversationID,
		AfterJSON: &detail,
	})
}

// RecordSkillTrigger 记录成功注入到运行时的 Skill 版本与触发来源。
// 审计写入失败只记日志，避免影响当前对话；Tool/Sandbox 的强一致执行审计另有专用链路。
func (s *Service) RecordSkillTrigger(
	ctx context.Context, workspaceID, conversationID, skillVersionID, skillName, source string,
) {
	store, ok := s.store.(SkillTriggerStore)
	if !ok {
		return
	}
	if err := store.InsertSkillTrigger(ctx, workspaceID, conversationID, skillVersionID, skillName, source); err != nil {
		log.Printf("insert skill trigger failed: conversation_id=%s skill=%s error=%v", conversationID, skillName, err)
	}
}

// Query 检索审计日志。
func (s *Service) Query(ctx context.Context, f QueryFilter) ([]*domain.AuditLog, error) {
	return s.store.Query(ctx, f)
}

// Writer 暴露底层 writer（供 Guard recorder 适配、优雅关闭）。
func (s *Service) Writer() *Writer { return s.writer }

// Close 关闭。
func (s *Service) Close() { s.writer.Close() }
