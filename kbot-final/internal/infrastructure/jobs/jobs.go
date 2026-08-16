// Package jobs 提供基于 asynq 的异步任务系统
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// RedisOpt 从 go-redis 客户端构造 asynq 的连接选项(client/server/scheduler 共用)。
func RedisOpt(redisClient *redis.Client) asynq.RedisClientOpt {
	opt := asynq.RedisClientOpt{
		Network: redisClient.Options().Network,
		Addr:    redisClient.Options().Addr,
		DB:      redisClient.Options().DB,
	}
	if redisClient.Options().Password != "" {
		opt.Password = redisClient.Options().Password
	}
	return opt
}

// Client 封装 asynq 客户端
type Client struct {
	*asynq.Client
}

// NewClient 创建任务客户端
func NewClient(redisClient *redis.Client) *Client {
	return &Client{Client: asynq.NewClient(RedisOpt(redisClient))}
}

// LarkReplyPayload 是飞书入站消息的持久化任务载荷。
type LarkReplyPayload struct {
	EventID string `json:"event_id"`
	AgentID string `json:"agent_id"`
	Channel string `json:"channel"`
	UserID  string `json:"user_id"`
	Text    string `json:"text"`
}

// LarkReplyEnqueuer 供 HTTP 入站层注入可测试的持久化投递能力。
type LarkReplyEnqueuer interface {
	EnqueueLarkReply(context.Context, LarkReplyPayload) error
}

// EnqueueLarkReply 把飞书回复放入 Redis 持久化队列；事件 ID 同时作为任务去重来源。
func (c *Client) EnqueueLarkReply(ctx context.Context, payload LarkReplyPayload) error {
	if c == nil || c.Client == nil {
		return fmt.Errorf("jobs client is not configured")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	replyID := LarkReplyID(payload.EventID)
	_, err = c.EnqueueContext(ctx, asynq.NewTask(TypeLarkReply, raw),
		asynq.TaskID("lark-reply-"+replyID),
		asynq.Queue("critical"),
		asynq.MaxRetry(8),
		asynq.Timeout(3*time.Minute),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// LarkReplyID 生成可同时用于 Asynq TaskID 和飞书消息 UUID 的稳定短 ID。
func LarkReplyID(eventID string) string {
	digest := sha256.Sum256([]byte(eventID))
	return hex.EncodeToString(digest[:16])
}

// NewScheduler 创建月分区维护使用的 cron 调度器。
func NewScheduler(redisClient *redis.Client) *asynq.Scheduler {
	return asynq.NewScheduler(RedisOpt(redisClient), nil)
}

// Server 封装 asynq 服务器
type Server struct {
	*asynq.Server
	mux *asynq.ServeMux
}

// NewServer 创建任务服务器
func NewServer(redisClient *redis.Client, concurrency int) *Server {
	opt := RedisOpt(redisClient)

	cfg := asynq.Config{
		Concurrency: concurrency,
		Queues: map[string]int{
			"critical": 6, // 高优先级
			"default":  3, // 默认
			"low":      1, // 低优先级
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			log.Printf("job failed: type=%s error=%v", task.Type(), err)
		}),
	}

	server := asynq.NewServer(opt, cfg)
	mux := asynq.NewServeMux()

	return &Server{
		Server: server,
		mux:    mux,
	}
}

// HandleFunc 注册任务处理函数
func (s *Server) HandleFunc(pattern string, handler asynq.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}

// Start 启动任务服务器
func (s *Server) Start() error {
	return s.Server.Run(s.mux)
}

// JobType 定义任务类型常量
const (
	TypeKBIngestDocument       = "kb_ingest_document"
	TypeKBReindex              = "kb_reindex"
	TypeKBRefreshConnector     = "kb_refresh_connector"
	TypeSkillEvaluate          = "skill_evaluate"
	TypeAgentEvalRun           = "agent_eval_run"
	TypeCostAggregation        = "cost_aggregation"
	TypeAuditExport            = "audit_export"
	TypeEngineResume           = "engine_resume" // 审批通过后续跑会话
	TypeApprovalResumeDispatch = "approval_resume_dispatch"
	TypeLarkReply              = "lark_reply"
)

// ResumePayload 是 engine_resume 任务载荷。
type ResumePayload struct {
	ConversationID string `json:"conversation_id"`
	ApprovalID     string `json:"approval_id"`
}
