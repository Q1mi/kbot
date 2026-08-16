package v1

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/Q1mi/kbot/internal/infrastructure/jobs"
	"github.com/Q1mi/kbot/internal/integration"
	larkintegration "github.com/Q1mi/kbot/internal/integration/lark"
	"github.com/Q1mi/kbot/internal/integration/webhook"
	"github.com/Q1mi/kbot/internal/runtime/cache"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type chatRunner interface {
	Chat(context.Context, engine.ChatStreamRequest) (string, error)
}

// IntegrationHandler 处理通用 Webhook 与飞书事件入站触发。
type IntegrationHandler struct {
	webhook    *webhook.Adapter
	lark       *larkintegration.Adapter
	larkAgent  string
	larkSender bool
	larkQueue  jobs.LarkReplyEnqueuer
	runtime    chatRunner
	replay     cache.IdemStore
}

// NewIntegrationHandler 创建集成处理器。
func NewIntegrationHandler(secret string, runtime *engine.Engine) *IntegrationHandler {
	return &IntegrationHandler{webhook: webhook.New(secret), runtime: runtime, replay: cache.NewMemoryIdemStore()}
}

func (h *IntegrationHandler) WithReplayStore(store cache.IdemStore) *IntegrationHandler {
	if store != nil {
		h.replay = store
	}
	return h
}

// WithLark 装配飞书入站验证与默认 Agent。
func (h *IntegrationHandler) WithLark(verifyToken, encryptKey, agentID string, outboundEnabled bool) *IntegrationHandler {
	h.lark = larkintegration.New(verifyToken, encryptKey)
	h.larkAgent = agentID
	h.larkSender = outboundEnabled
	return h
}

// WithLarkQueue 注入持久化任务队列；HTTP 回调只在投递成功后确认事件。
func (h *IntegrationHandler) WithLarkQueue(queue jobs.LarkReplyEnqueuer) *IntegrationHandler {
	h.larkQueue = queue
	return h
}

// Webhook 外部系统调 webhook → 触发一个 Agent → 返回结果。
func (h *IntegrationHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := readIntegrationBody(w, r)
	if err != nil {
		return
	}
	// 签名校验（防伪造）。
	if err := h.webhook.Verify(r.Header, body); err != nil {
		if errors.Is(err, webhook.ErrNotConfigured) {
			http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	nonce := r.Header.Get("X-Kbot-Nonce")
	replayKey := "integration:webhook:" + nonce
	accepted, err := h.replay.SetIfAbsent(r.Context(), replayKey, []byte(`{"status":"processing"}`), 10*time.Minute)
	if err != nil {
		http.Error(w, "webhook replay store unavailable", http.StatusServiceUnavailable)
		return
	}
	if !accepted {
		if cached, ok, getErr := h.replay.Get(r.Context(), replayKey); getErr == nil && ok {
			var response map[string]string
			if json.Unmarshal(cached, &response) == nil {
				if response["reply"] != "" {
					w.Header().Set("X-Idempotent-Replay", "true")
					writeJSON(w, http.StatusOK, response)
					return
				}
				if response["status"] == "processing" {
					writeJSON(w, http.StatusConflict, response)
					return
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	releaseReplay := func() { _ = h.replay.Delete(context.WithoutCancel(r.Context()), replayKey) }
	// 归一化。
	var in *integration.Inbound
	if in, err = h.webhook.Translate(body); err != nil {
		releaseReplay()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 触发 Agent（Channel 承载 agent_id）。
	reply, err := h.runtime.Chat(r.Context(), engine.ChatStreamRequest{
		AgentID: in.Channel, WorkspaceID: in.WorkspaceID, Message: in.Text, UserID: in.UserID,
	})
	if err != nil {
		releaseReplay()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := map[string]string{"reply": reply}
	encoded, _ := json.Marshal(response)
	if err := h.replay.Set(context.WithoutCancel(r.Context()), replayKey, encoded, 10*time.Minute); err != nil {
		log.Printf("cache webhook response %s: %v", nonce, err)
	}
	writeJSON(w, http.StatusOK, response)
}

// LarkEvents 接收飞书 URL 校验与 im.message.receive_v1 事件。
// 消息事件先快速确认，再异步触发 Agent 并把回答发送回原会话，避免飞书回调超时重试。
// @Summary  飞书事件订阅与 URL 校验
// @Tags     integrations
// @Accept   json
// @Produce  json
// @Success  200  {object}  map[string]string
// @Success  202  {object}  map[string]string
// @Router   /integrations/lark/events [post]
func (h *IntegrationHandler) LarkEvents(w http.ResponseWriter, r *http.Request) {
	if h.lark == nil {
		http.Error(w, "lark not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := readIntegrationBody(w, r)
	if err != nil {
		return
	}
	if err := h.lark.Verify(r.Header, body); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	in, err := h.lark.Translate(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if in.Challenge != "" {
		writeJSON(w, http.StatusOK, map[string]string{"challenge": in.Challenge})
		return
	}
	if h.larkAgent == "" || h.larkQueue == nil || !h.larkSender {
		http.Error(w, "lark agent, outbound credentials or durable queue not configured", http.StatusServiceUnavailable)
		return
	}
	if in.EventID == "" {
		http.Error(w, "lark event_id is required", http.StatusBadRequest)
		return
	}
	duplicate, err := h.markLarkEvent(r.Context(), in.EventID)
	if err != nil {
		http.Error(w, "lark replay store unavailable", http.StatusServiceUnavailable)
		return
	}
	if duplicate {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	if err := h.larkQueue.EnqueueLarkReply(r.Context(), jobs.LarkReplyPayload{
		EventID: in.EventID, AgentID: h.larkAgent, Channel: in.Channel, UserID: in.UserID, Text: in.Text,
	}); err != nil {
		_ = h.replay.Delete(context.WithoutCancel(r.Context()), "integration:lark:"+in.EventID)
		http.Error(w, "lark queue unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "event_id": in.EventID})
}

// markLarkEvent 返回事件是否已经进入持久化队列。生产环境使用 Redis 跨进程去重。
func (h *IntegrationHandler) markLarkEvent(ctx context.Context, eventID string) (bool, error) {
	if eventID == "" {
		return false, nil
	}
	accepted, err := h.replay.SetIfAbsent(ctx, "integration:lark:"+eventID, []byte("queued"), 24*time.Hour)
	return !accepted, err
}

func readIntegrationBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	if err == nil {
		return body, nil
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return nil, err
	}
	http.Error(w, "read body", http.StatusBadRequest)
	return nil, err
}
