// Package kb 提供知识库管理与 ingest 管道（设计文档 §4.5 / 讲义 §14.3）。
package kb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Q1mi/kbot/internal/connector"
	"github.com/Q1mi/kbot/internal/connector/markdown_folder"
	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
	"github.com/Q1mi/kbot/internal/util"
)

const maxDocumentBytes = 10 << 20

// TaskEnqueuer 是 kb.Service 投递异步 ingest 的最小接口(jobs.Client 满足之)。
// 为 nil 时 SyncMarkdownFolder 退回进程内同步 ingest(单测 / e2e / 无 Redis)。
type TaskEnqueuer interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// Store 是 KB 的存储接口。
type Store interface {
	CreateKB(ctx context.Context, kb *domain.KnowledgeBase) error
	GetKB(ctx context.Context, kbID string) (*domain.KnowledgeBase, error)
	ListKBs(ctx context.Context, workspaceID string) ([]*domain.KnowledgeBase, error)
	UpdateKBStatus(ctx context.Context, kbID, status string) error

	UpsertDocument(ctx context.Context, doc *domain.KbDocument) error
	GetDocument(ctx context.Context, docID string) (*domain.KbDocument, error)
	ListDocuments(ctx context.Context, kbID string) ([]*domain.KbDocument, error)

	CreateIngestJob(ctx context.Context, job *domain.KbIngestJob) error
	UpdateIngestJob(ctx context.Context, job *domain.KbIngestJob) error
	ListIngestJobs(ctx context.Context, kbID string) ([]*domain.KbIngestJob, error)

	UpsertConnector(ctx context.Context, c *domain.ConnectorInstance) error
	ListConnectors(ctx context.Context, kbID string) ([]*domain.ConnectorInstance, error)
}

// Service KB 服务。
type Service struct {
	store                Store
	retriever            retriever.Searcher // 内存版或 pgvector 版
	enqueuer             TaskEnqueuer       // 非 nil 则 Sync 走异步(worker ingest);nil 则进程内同步
	chunkSize            int
	overlap              int
	markdownAllowedRoots []string
}

// NewService 创建 KB 服务。enqueuer 可为 nil(单测 / e2e 走同步 ingest)。
func NewService(store Store, r retriever.Searcher, enqueuer TaskEnqueuer) *Service {
	return &Service{store: store, retriever: r, enqueuer: enqueuer, chunkSize: 500, overlap: 100}
}

// ConfigureMarkdownAllowedRoots 设置 HTTP Connector 可以读取的服务端目录根。
// 内部静态资料导入不经过该入口。
func (s *Service) ConfigureMarkdownAllowedRoots(roots []string) {
	s.markdownAllowedRoots = append([]string(nil), roots...)
}

// CreateKBRequest 创建知识库请求。
type CreateKBRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	EmbeddingModel string `json:"embedding_model"`
	CreatedBy      string `json:"created_by"`
}

// CreateKB 创建知识库。
func (s *Service) CreateKB(ctx context.Context, req CreateKBRequest) (*domain.KnowledgeBase, error) {
	kb := &domain.KnowledgeBase{
		ID:             util.GenerateID(),
		WorkspaceID:    req.WorkspaceID,
		Name:           req.Name,
		ChunkingConfig: `{"size":500,"overlap":100}`,
		EmbeddingModel: req.EmbeddingModel,
		Status:         "active",
		CreatedBy:      req.CreatedBy,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := s.store.CreateKB(ctx, kb); err != nil {
		return nil, fmt.Errorf("create kb: %w", err)
	}
	return kb, nil
}

// ListKBs 列出知识库。
func (s *Service) ListKBs(ctx context.Context, workspaceID string) ([]*domain.KnowledgeBase, error) {
	return s.store.ListKBs(ctx, workspaceID)
}

// KBExists 为 Skill 发布门禁校验知识库 ID 与工作空间归属。
func (s *Service) KBExists(ctx context.Context, workspaceID, kbID string) (bool, error) {
	base, err := s.store.GetKB(ctx, kbID)
	if err != nil {
		return false, fmt.Errorf("get kb: %w", err)
	}
	return base.WorkspaceID == workspaceID, nil
}

// EnsureKBWorkspace 校验知识库属于当前工作空间。
func (s *Service) EnsureKBWorkspace(ctx context.Context, kbID, workspaceID string) error {
	base, err := s.store.GetKB(ctx, kbID)
	if err != nil {
		return fmt.Errorf("get kb: %w", err)
	}
	if workspaceID == "" || base.WorkspaceID != workspaceID {
		return fmt.Errorf("knowledge base does not belong to current workspace")
	}
	return nil
}

// AddMarkdownFolder 给 KB 绑定一个本地 Markdown 文件夹 connector（reference）。
func (s *Service) AddMarkdownFolder(ctx context.Context, kbID, rootPath string) (*domain.ConnectorInstance, error) {
	if _, err := s.store.GetKB(ctx, kbID); err != nil {
		return nil, fmt.Errorf("get kb: %w", err)
	}
	ci := &domain.ConnectorInstance{
		ID:            util.GenerateID(),
		KbID:          kbID,
		ConnectorKind: "markdown_folder",
		ConfigJSON:    fmt.Sprintf(`{"root_path":%q}`, rootPath),
		CreatedAt:     time.Now(),
	}
	if err := s.store.UpsertConnector(ctx, ci); err != nil {
		return nil, fmt.Errorf("upsert connector: %w", err)
	}
	return ci, nil
}

// SyncResult 汇总一次连接器同步的结果。
type SyncResult struct {
	Listed   int `json:"listed"`
	Ingested int `json:"ingested"`
	Skipped  int `json:"skipped"` // hash 未变，跳过
}

// SyncMarkdownFolder 扫描指定路径的 Markdown 文件夹，对新增/变更的文档跑 ingest。
// 直接传 rootPath，便于 API / 测试使用;实际部署可从 ConnectorInstance.config 解析。
func (s *Service) SyncMarkdownFolder(ctx context.Context, kbID, rootPath string) (*SyncResult, error) {
	conn := markdown_folder.New(rootPath)
	return s.syncConnector(ctx, kbID, conn)
}

// SyncMarkdownFolderAllowed 为外部 API 提供受根目录约束的 Markdown 同步。
func (s *Service) SyncMarkdownFolderAllowed(ctx context.Context, kbID, rootPath string) (*SyncResult, error) {
	resolved, err := s.allowedMarkdownRoot(rootPath)
	if err != nil {
		return nil, err
	}
	return s.SyncMarkdownFolder(ctx, kbID, resolved)
}

func (s *Service) allowedMarkdownRoot(requested string) (string, error) {
	if strings.TrimSpace(requested) == "" || len(s.markdownAllowedRoots) == 0 {
		return "", fmt.Errorf("markdown connector root is not allowed")
	}
	requestedAbs, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve markdown connector root: %w", err)
	}
	requestedReal, err := filepath.EvalSymlinks(requestedAbs)
	if err != nil {
		return "", fmt.Errorf("resolve markdown connector root: %w", err)
	}
	for _, allowed := range s.markdownAllowedRoots {
		allowedAbs, absErr := filepath.Abs(strings.TrimSpace(allowed))
		if absErr != nil {
			continue
		}
		allowedReal, evalErr := filepath.EvalSymlinks(allowedAbs)
		if evalErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(allowedReal, requestedReal)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return requestedReal, nil
		}
	}
	return "", fmt.Errorf("markdown connector root is outside configured allowed roots")
}

// StaticDocument 是由代码或安装包携带的一份可重复同步的 Markdown 文档。
// SourceURI 在同一知识库内保持稳定，内容 Hash 变化时会触发增量 ingest。
type StaticDocument struct {
	SourceURI string
	Title     string
	Content   string
}

// SyncStaticDocuments 把内嵌课程资料等静态 Markdown 内容接入统一 Connector ingest 管道。
func (s *Service) SyncStaticDocuments(ctx context.Context, kbID string, documents []StaticDocument) (*SyncResult, error) {
	conn, err := newStaticConnector(documents)
	if err != nil {
		return nil, err
	}
	return s.syncConnector(ctx, kbID, conn)
}

type staticConnector struct {
	documents map[string]StaticDocument
	metas     []connector.DocMeta
}

func newStaticConnector(documents []StaticDocument) (*staticConnector, error) {
	conn := &staticConnector{documents: make(map[string]StaticDocument, len(documents))}
	for _, document := range documents {
		if document.SourceURI == "" || document.Title == "" || document.Content == "" {
			return nil, fmt.Errorf("static document source_uri, title and content are required")
		}
		if _, exists := conn.documents[document.SourceURI]; exists {
			return nil, fmt.Errorf("duplicate static document source_uri %q", document.SourceURI)
		}
		conn.documents[document.SourceURI] = document
		hash := sha256.Sum256([]byte(document.Content))
		conn.metas = append(conn.metas, connector.DocMeta{
			ID: document.SourceURI, Title: document.Title, Hash: hex.EncodeToString(hash[:]),
		})
	}
	sort.Slice(conn.metas, func(i, j int) bool { return conn.metas[i].ID < conn.metas[j].ID })
	return conn, nil
}

func (c *staticConnector) Name() string { return "static_markdown" }

func (c *staticConnector) ListDocuments(context.Context, string) ([]connector.DocMeta, string, error) {
	return append([]connector.DocMeta(nil), c.metas...), "", nil
}

func (c *staticConnector) FetchDocument(ctx context.Context, documentID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, ok := c.documents[documentID]
	if !ok {
		return nil, fmt.Errorf("static document %q not found", documentID)
	}
	return io.NopCloser(bytes.NewBufferString(document.Content)), nil
}

func (s *Service) syncConnector(ctx context.Context, kbID string, conn connector.Connector) (*SyncResult, error) {
	if _, err := s.store.GetKB(ctx, kbID); err != nil {
		return nil, fmt.Errorf("get kb: %w", err)
	}

	metas, _, err := conn.ListDocuments(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}

	res := &SyncResult{Listed: len(metas)}
	for _, m := range metas {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		existing, err := s.store.GetDocument(ctx, docID(kbID, m.ID))
		if err != nil {
			return res, fmt.Errorf("get document %s: %w", m.ID, err)
		}
		if existing != nil && existing.Hash == m.Hash && existing.Status == "processed" {
			res.Skipped++
			continue
		}

		rc, err := conn.FetchDocument(ctx, m.ID)
		if err != nil {
			return res, fmt.Errorf("fetch document %s: %w", m.ID, err)
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxDocumentBytes+1))
		closeErr := rc.Close()
		if err != nil {
			return res, fmt.Errorf("read document %s: %w", m.ID, err)
		}
		if closeErr != nil {
			return res, fmt.Errorf("close document %s: %w", m.ID, closeErr)
		}
		if len(content) > maxDocumentBytes {
			return res, fmt.Errorf("document %s exceeds %d bytes", m.ID, maxDocumentBytes)
		}

		doc := &domain.KbDocument{
			ID:             docID(kbID, m.ID),
			KbID:           kbID,
			SourceType:     "connector",
			SourceURI:      m.ID,
			Hash:           m.Hash,
			Classification: "internal",
			Status:         "pending",
			CreatedAt:      time.Now(),
		}
		if err := s.store.UpsertDocument(ctx, doc); err != nil {
			return res, fmt.Errorf("upsert document: %w", err)
		}

		if s.enqueuer != nil {
			// 跨进程闭环:投递到 worker 异步 ingest(写 kb_chunks),server 检索直接读库。
			task, err := NewIngestTask(IngestPayload{KbID: kbID, DocumentID: doc.ID, Content: string(content)})
			if err != nil {
				return res, fmt.Errorf("build ingest task %s: %w", m.ID, err)
			}
			if _, err := s.enqueuer.Enqueue(task); err != nil {
				return res, fmt.Errorf("enqueue ingest %s: %w", m.ID, err)
			}
		} else {
			// 同步管道(无 Redis / 单测):直接在本进程 ingest。
			if err := s.IngestDocument(ctx, kbID, doc.ID, string(content)); err != nil {
				return res, fmt.Errorf("ingest document %s: %w", m.ID, err)
			}
		}
		res.Ingested++
	}
	return res, nil
}

// docID 由 kbID + 来源路径派生稳定 ID，保证同一文档重复同步指向同一记录。
func docID(kbID, sourceURI string) string {
	h := sha256.Sum256([]byte(kbID + "\x00" + sourceURI))
	return hex.EncodeToString(h[:16])
}

// Search 在 KB 上做混合检索。
func (s *Service) Search(ctx context.Context, kbID, query string, k int) ([]retriever.Passage, error) {
	return s.retriever.Search(ctx, kbID, query, k)
}

// SearchMode 按 bm25、vector 或 hybrid 模式检索。
// 若底层 retriever 未实现 ModeSearcher,回退到默认 Hybrid Search(三档结果相同)。
func (s *Service) SearchMode(ctx context.Context, kbID, query string, k int, mode string) ([]retriever.Passage, error) {
	if ms, ok := s.retriever.(retriever.ModeSearcher); ok {
		return ms.SearchMode(ctx, kbID, query, k, mode)
	}
	return s.retriever.Search(ctx, kbID, query, k)
}

// ListDocuments 列出知识库文档。
func (s *Service) ListDocuments(ctx context.Context, kbID string) ([]*domain.KbDocument, error) {
	return s.store.ListDocuments(ctx, kbID)
}

// ListConnectors 列出知识库连接器实例。
func (s *Service) ListConnectors(ctx context.Context, kbID string) ([]*domain.ConnectorInstance, error) {
	return s.store.ListConnectors(ctx, kbID)
}
