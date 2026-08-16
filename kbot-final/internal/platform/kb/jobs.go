package kb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// TypeIngestDocument 是 KB 文档 ingest 的 asynq 任务类型。
const TypeIngestDocument = "kb_ingest_document"

// IngestPayload 是异步 ingest 任务的载荷。
type IngestPayload struct {
	KbID       string `json:"kb_id"`
	DocumentID string `json:"document_id"`
	Content    string `json:"content"`
}

// NewIngestTask 构造一个 ingest 任务。
//
// 幂等键用 (kb_id, document_id)：asynq 在 TaskID 去重窗口内不会重复入队同一文档
// （设计文档 §4.10 的幂等思想）。
func NewIngestTask(p IngestPayload) (*asynq.Task, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	taskID := fmt.Sprintf("ingest:%s:%s", p.KbID, p.DocumentID)
	return asynq.NewTask(TypeIngestDocument, b, asynq.TaskID(taskID), asynq.Queue("default")), nil
}

// HandleIngest 返回处理 ingest 任务的 asynq.HandlerFunc。失败返回 error，
// 由 asynq 按退避重试（§4.10）。
func (s *Service) HandleIngest() asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p IngestPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("unmarshal ingest payload: %w", err)
		}
		return s.IngestDocument(ctx, p.KbID, p.DocumentID, p.Content)
	}
}
