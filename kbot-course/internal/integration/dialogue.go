package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type Reply struct {
	Text, ConversationID, AgentVersionID, Status, ApprovalID string
}

type dialogueSession struct {
	conversationID string
	lastUsed       time.Time
}

// Dialogue 将已验证的渠道身份映射到固定运行目标。会话表为单进程内存状态。
// 串行执行保证同一会话的历史追加顺序；空闲 24 小时或容量淘汰后创建新会话。
type Dialogue struct {
	mu                                sync.Mutex
	agents                            *agent.Service
	runtime                           ChatRuntime
	workspaceID, agentID, environment string
	sessions                          map[string]dialogueSession
}

func NewDialogue(agents *agent.Service, runtime ChatRuntime, workspaceID, agentID, environment string) (*Dialogue, error) {
	if agents == nil || runtime == nil || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("channel runtime and fixed workspace/agent mapping are required")
	}
	if environment == "" {
		environment = "prod"
	}
	return &Dialogue{agents: agents, runtime: runtime, workspaceID: workspaceID, agentID: agentID, environment: environment, sessions: make(map[string]dialogueSession)}, nil
}

func (d *Dialogue) Respond(ctx context.Context, message Message) (Reply, error) {
	if message.Source == "" || message.AppID == "" || message.TenantKey == "" || message.ChatID == "" || message.UserID == "" || strings.TrimSpace(message.Text) == "" {
		return Reply{}, fmt.Errorf("verified channel identity and text are required")
	}
	// JSON 编码避免使用分隔符拼接身份时发生歧义。聊天也属于会话所有者作用域。
	identity, _ := json.Marshal([]string{message.Source, message.AppID, message.TenantKey, message.ChatID, message.UserID})
	digest := sha256.Sum256(identity)
	userID := "channel:" + message.Source + ":" + hex.EncodeToString(digest[:])
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for key, session := range d.sessions {
		if now.Sub(session.lastUsed) > 24*time.Hour {
			delete(d.sessions, key)
		}
	}
	session := d.sessions[userID]
	reset := strings.TrimSpace(message.Text) == "/new"
	if reset {
		session.conversationID = ""
	}
	// workspace、agent 和 conversation 均由服务端决定，不采信消息载荷中的对应字段。
	conversation, err := d.agents.ResolveConversation(ctx, d.workspaceID, userID, d.agentID, d.environment, session.conversationID)
	if err != nil {
		return Reply{}, err
	}
	if _, exists := d.sessions[userID]; !exists && len(d.sessions) >= 4096 {
		oldestKey := ""
		oldestTime := now
		for key, item := range d.sessions {
			if item.lastUsed.Before(oldestTime) {
				oldestKey, oldestTime = key, item.lastUsed
			}
		}
		delete(d.sessions, oldestKey)
	}
	d.sessions[userID] = dialogueSession{conversationID: conversation.ID, lastUsed: now}
	reply := Reply{ConversationID: conversation.ID, AgentVersionID: conversation.AgentVersionID}
	if reset {
		reply.Text, reply.Status = "已开启新会话，请发送问题。", "completed"
		return reply, nil
	}
	err = d.runtime.ChatStream(ctx, engine.ChatRequest{
		ConversationID: conversation.ID, WorkspaceID: d.workspaceID, UserID: userID,
		Message: message.Text, AgentEnvironment: d.environment,
	}, func(event engine.Event) error {
		switch event.Type {
		case "answer_done":
			reply.Text = event.Text
		case "approval_requested":
			if data, ok := event.Data.(map[string]string); ok {
				reply.ApprovalID = data["approval_id"]
			}
		case "run_finished":
			if data, ok := event.Data.(map[string]string); ok {
				reply.Status = data["status"]
			}
		}
		return nil
	})
	if err != nil {
		return reply, err
	}
	switch reply.Status {
	case "blocked":
		reply.Text = "本次请求被安全规则拦截，请调整问题后重试。"
	case "awaiting_approval":
		reply.Text = "操作正在等待审批，请联系管理员处理。审批编号：" + reply.ApprovalID
	}
	if strings.TrimSpace(reply.Text) == "" {
		reply.Text = "本次未生成文本回复，请稍后重试。"
	}
	return reply, nil
}
