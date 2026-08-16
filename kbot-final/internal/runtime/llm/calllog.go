package llm

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CallUsage 是一次模型调用的计量(写入 model_call_logs)。
// CachedTokens 即 Prompt Cache 命中量(OpenAI 兼容从 usage.cached_tokens 读回;Anthropic 为 cache_read)。
type CallUsage struct {
	Provider              string
	ProviderID            string
	DeploymentID          string
	Model                 string
	WorkspaceID           string
	AgentID               string
	UserID                string
	PromptVersionID       string
	ModelProfileVersionID string
	ExperimentID          string
	ExperimentVariant     string
	InputTokens           int
	OutputTokens          int
	CachedTokens          int
	Cost                  float64
	LatencyMs             int
	Status                string // ok | error
	Classification        string
}

// ModelCallSink 接收每次模型调用的计量。db != nil 时写 model_call_logs;否则 NopSink。
type ModelCallSink interface {
	Record(ctx context.Context, u CallUsage)
}

// NopSink 丢弃计量(无 DB / 测试)。
type NopSink struct{}

func (NopSink) Record(context.Context, CallUsage) {}

// PgModelCallSink 把计量写入 model_call_logs 月分区父表。
type PgModelCallSink struct {
	db *pgxpool.Pool
}

// NewPgModelCallSink 创建 PG 计量落库器。
func NewPgModelCallSink(db *pgxpool.Pool) *PgModelCallSink {
	return &PgModelCallSink{db: db}
}

func (s *PgModelCallSink) Record(ctx context.Context, u CallUsage) {
	classification := u.Classification
	if classification == "" {
		classification = "internal"
	}
	status := u.Status
	if status == "" {
		status = "ok"
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO model_call_logs
		  (agent_id,user_id,provider_id,model,input_tokens,output_tokens,cached_tokens,cost,
		   latency_ms,status,classification,workspace_id,prompt_version_id,
		   model_profile_version_id,deployment_id,experiment_id,experiment_variant,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,now())`,
		nullableUUID(u.AgentID), nullableUUID(u.UserID), nullableUUID(u.ProviderID), u.Model,
		u.InputTokens, u.OutputTokens, u.CachedTokens, u.Cost, u.LatencyMs, status, classification,
		u.WorkspaceID, nullableUUID(u.PromptVersionID), nullableUUID(u.ModelProfileVersionID),
		nullableUUID(u.DeploymentID), nullableUUID(u.ExperimentID), u.ExperimentVariant)
	if err != nil {
		// 计量落库失败不应影响主对话:记日志即可。
		log.Printf("model_call_logs insert: %v", err)
	}
}

func nullableUUID(v string) any {
	id, err := uuid.Parse(v)
	if err != nil {
		return nil
	}
	return id
}
