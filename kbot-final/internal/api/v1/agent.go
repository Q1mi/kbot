package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Q1mi/kbot/internal/a2ui"
	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/approval"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/go-chi/chi/v5"
)

// AgentHandler Agent处理器
type AgentHandler struct {
	agentService *agent.Service
	runtime      *engine.Engine
	approvals    approval.Store
}

// NewAgentHandler 创建Agent处理器
func NewAgentHandler(agentService *agent.Service, runtime *engine.Engine, approvals ...approval.Store) *AgentHandler {
	handler := &AgentHandler{
		agentService: agentService,
		runtime:      runtime,
	}
	if len(approvals) > 0 {
		handler.approvals = approvals[0]
	}
	return handler
}

// CreateAgent 创建Agent
// @Summary  创建 Agent
// @Tags     agents
// @Security BearerAuth
// @Param    body  body      agent.CreateAgentRequest  true  "Agent 定义"
// @Success  200   {object}  map[string]interface{}
// @Router   /agents [post]
func (h *AgentHandler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())

	var req agent.CreateAgentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.CreatedBy = userID
	req.WorkspaceID = workspaceID

	ag, err := h.agentService.CreateAgent(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, ag)
}

// ListAgents 列出当前工作空间的 Agents。
// @Summary  列出 Agents
// @Tags     agents
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /agents [get]
func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	ags, err := h.agentService.ListAgents(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ags)
}

// GetAgent 返回单个 Agent 详情。
// @Summary  Agent 详情
// @Tags     agents
// @Security BearerAuth
// @Param    agent_id  path      string  true  "Agent ID"
// @Success  200       {object}  map[string]interface{}
// @Router   /agents/{agent_id} [get]
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	ag, err := h.agentService.GetAgentForWorkspace(
		r.Context(), agentID, middleware.GetWorkspaceIDFromContext(r.Context()),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, ag)
}

// ListAgentVersions 列出 Agent 的不可变配置版本。
// @Summary  列出 Agent 不可变版本
// @Tags     agents
// @Security BearerAuth
// @Param    agent_id  path      string  true  "Agent ID"
// @Success  200       {array}   agent.AgentVersionView
// @Router   /agents/{agent_id}/versions [get]
func (h *AgentHandler) ListAgentVersions(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	versions, err := h.agentService.ListAgentVersions(r.Context(), agentID, workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

// CreateAgentVersion 保存一个新快照，并自动绑定到 dev。
// @Summary  创建 Agent 不可变版本
// @Tags     agents
// @Security BearerAuth
// @Param    agent_id  path      string                    true  "Agent ID"
// @Param    body      body      agent.AgentVersionConfig  true  "版本配置"
// @Success  201       {object}  agent.AgentVersionView
// @Router   /agents/{agent_id}/versions [post]
func (h *AgentHandler) CreateAgentVersion(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	userID := middleware.GetUserIDFromContext(r.Context())
	var cfg agent.AgentVersionConfig
	if err := decodeJSON(w, r, &cfg); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	version, err := h.agentService.CreateAgentVersion(r.Context(), agentID, workspaceID, cfg, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

// PromoteAgentVersionRequest Agent 版本环境晋升请求。
type PromoteAgentVersionRequest struct {
	Env       string `json:"env"`
	VersionID string `json:"version_id"`
}

// PromoteAgentVersion 把一个已有版本绑定到目标环境。
// @Summary  晋升 Agent 版本到指定环境
// @Tags     agents
// @Security BearerAuth
// @Param    agent_id  path      string                      true  "Agent ID"
// @Param    body      body      PromoteAgentVersionRequest  true  "环境晋升"
// @Success  200       {object}  map[string]string
// @Router   /agents/{agent_id}/promote [post]
func (h *AgentHandler) PromoteAgentVersion(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	var body PromoteAgentVersionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.agentService.PromoteAgentVersion(r.Context(), agentID, workspaceID, body.Env, body.VersionID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "promoted", "env": body.Env})
}

// GetConversation 返回会话详情与历史消息。
// @Summary  会话详情 + 历史消息
// @Tags     conversations
// @Security BearerAuth
// @Param    conversation_id  path      string  true  "会话 ID"
// @Success  200              {object}  map[string]interface{}
// @Router   /conversations/{conversation_id} [get]
func (h *AgentHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := chi.URLParam(r, "conversation_id")
	conv, err := h.agentService.LoadConversationForUser(
		r.Context(), conversationID,
		middleware.GetWorkspaceIDFromContext(r.Context()), middleware.GetUserIDFromContext(r.Context()),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	msgs, err := h.agentService.LoadConversationMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	traceID := ""
	if conv.RuntimeConfigJSON != "" {
		var runtimeConfig domain.ConversationRuntimeConfig
		if err := json.Unmarshal([]byte(conv.RuntimeConfigJSON), &runtimeConfig); err != nil {
			http.Error(w, "decode conversation runtime config", http.StatusInternalServerError)
			return
		}
		traceID = runtimeConfig.LatestTraceID
	}
	surfaces, err := h.historicalA2UISurfaces(r.Context(), conv.ID, conv.AgentVersionID, msgs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation":  conv,
		"messages":      msgs,
		"trace_id":      traceID,
		"a2ui_surfaces": surfaces,
	})
}

type historicalA2UISurface struct {
	CreatedAt time.Time      `json:"created_at"`
	Messages  []a2ui.Message `json:"messages"`
}

func (h *AgentHandler) historicalA2UISurfaces(
	ctx context.Context, conversationID, agentVersionID string, conversationMessages []*domain.Message,
) ([]historicalA2UISurface, error) {
	if h.approvals == nil {
		return []historicalA2UISurface{}, nil
	}
	approvals, err := h.approvals.ListByConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	presentations := map[string]a2ui.ApprovalPresentation{}
	if h.runtime != nil {
		// Tool Version 可能已被管理员移除；卡片数据仍可使用通用展示降级恢复。
		if resolved, resolveErr := h.runtime.ApprovalPresentations(ctx, agentVersionID); resolveErr == nil {
			presentations = resolved
		}
	}
	surfaces := make([]historicalA2UISurface, 0, len(approvals))
	for _, item := range approvals {
		messages, err := a2ui.ApprovalSurfaceWithPresentation(
			item.ID, conversationID, item.Action, item.Payload, presentations[item.Action],
		)
		if err != nil {
			return nil, err
		}
		status := item.Status
		if status == approval.StatusApproved && item.ResolvedAt != nil {
			for _, message := range conversationMessages {
				if message.Role == "assistant" && message.CreatedAt.After(*item.ResolvedAt) {
					status = "completed"
					break
				}
			}
		}
		if status != approval.StatusPending {
			messages = append(messages, a2ui.ApprovalStatusMessages(item.ID, status)...)
		}
		if status == "completed" {
			messages = append(messages, a2ui.Message{
				Version: a2ui.Version,
				UpdateDataModel: &a2ui.UpdateDataModel{
					SurfaceID: "approval-" + item.ID,
					Path:      "/result_label",
					Value:     "Agent 已完成业务操作并返回最终结果",
				},
			})
		}
		createdAt := item.CreatedAt
		// Runtime 先创建审批记录，再在暂停分支落库本轮 user 消息。
		// 历史时间线把卡片放在紧随其后的触发消息后面，与实时 SSE 顺序一致。
		for _, conversationMessage := range conversationMessages {
			if conversationMessage.Role == "user" && !conversationMessage.CreatedAt.Before(item.CreatedAt) {
				createdAt = conversationMessage.CreatedAt.Add(time.Nanosecond)
				break
			}
		}
		surfaces = append(surfaces, historicalA2UISurface{CreatedAt: createdAt, Messages: messages})
	}
	return surfaces, nil
}

// ListConversations 返回当前登录用户的会话历史。
// @Summary  列出当前用户会话历史
// @Tags     conversations
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /conversations [get]
func (h *AgentHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	limit, offset := int32(30), int32(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = int32(parsed)
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			offset = int32(parsed)
		}
	}
	conversations, err := h.agentService.ListConversations(
		r.Context(), middleware.GetWorkspaceIDFromContext(r.Context()), middleware.GetUserIDFromContext(r.Context()),
		r.URL.Query().Get("agent_id"), limit, offset,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Message             string         `json:"message"`
	ConversationID      string         `json:"conversation_id,omitempty"`
	AgentEnv            string         `json:"agent_env,omitempty"`
	UserPromptVersionID string         `json:"user_prompt_version_id,omitempty"`
	UserPromptVariables map[string]any `json:"user_prompt_variables,omitempty"`
}

// GetUserPromptInputSpec 返回 Agent 当前环境绑定的首轮任务模板 Schema。
// @Summary  获取 Agent 首轮 User Prompt Template 输入规范
// @Tags     agents
// @Security BearerAuth
// @Param    agent_id   path      string  true   "Agent ID"
// @Param    agent_env  query     string  false  "Agent 环境，默认 dev"
// @Success  200        {object}  agent.UserPromptInputSpec
// @Router   /agents/{agent_id}/input-schema [get]
func (h *AgentHandler) GetUserPromptInputSpec(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	userID := middleware.GetUserIDFromContext(r.Context())
	spec, err := h.agentService.GetUserPromptInputSpec(
		r.Context(), agentID, workspaceID, r.URL.Query().Get("agent_env"), userID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

// Chat 同步聊天
// @Summary  与 Agent 同步对话(聚合流式为整段回答)
// @Tags     agents
// @Security BearerAuth
// @Param    agent_id  path      string       true  "Agent ID"
// @Param    body      body      ChatRequest  true  "消息"
// @Success  200       {object}  map[string]interface{}
// @Router   /agents/{agent_id}/chat [post]
func (h *AgentHandler) Chat(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	userID := middleware.GetUserIDFromContext(r.Context())

	var req ChatRequest
	if err := decodeJSON(w, r, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 创建流式请求
	streamReq := engine.ChatStreamRequest{
		AgentID:             agentID,
		WorkspaceID:         middleware.GetWorkspaceIDFromContext(r.Context()),
		AgentEnv:            req.AgentEnv,
		ConversationID:      req.ConversationID,
		Message:             req.Message,
		UserID:              userID,
		UserPromptVersionID: req.UserPromptVersionID,
		UserPromptVariables: req.UserPromptVariables,
	}

	eventCh, err := h.runtime.ChatStream(r.Context(), streamReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 收集所有事件，组装为同步响应
	var responseText string
	var started engine.RunStarted
	var approvalID string
	for event := range eventCh {
		switch event.Type {
		case engine.EventStarted:
			if value, ok := event.Data.(engine.RunStarted); ok {
				started = value
			}
		case engine.EventAnswerDelta:
			responseText += event.Text
		case engine.EventAwaitApproval:
			approvalID = event.Text
		case engine.EventError:
			http.Error(w, event.Text, http.StatusInternalServerError)
			return
		}
	}

	response := map[string]interface{}{
		"content":         responseText,
		"conversation_id": started.ConversationID,
		"trace_id":        started.TraceID,
	}
	if approvalID != "" {
		response["approval_id"] = approvalID
		response["status"] = "awaiting_approval"
	} else {
		response["status"] = "completed"
	}

	writeJSON(w, http.StatusOK, response)
}
