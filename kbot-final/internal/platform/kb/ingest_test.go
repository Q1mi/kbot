package kb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/retriever"
)

// memStore 是测试用的最小内存 KB 存储。
type memStore struct {
	mu         sync.Mutex
	kbs        map[string]*domain.KnowledgeBase
	docs       map[string]*domain.KbDocument
	jobs       map[string][]*domain.KbIngestJob
	connectors map[string][]*domain.ConnectorInstance
}

func newMemStore() *memStore {
	return &memStore{
		kbs:        map[string]*domain.KnowledgeBase{},
		docs:       map[string]*domain.KbDocument{},
		jobs:       map[string][]*domain.KbIngestJob{},
		connectors: map[string][]*domain.ConnectorInstance{},
	}
}

func (s *memStore) CreateKB(_ context.Context, kb *domain.KnowledgeBase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kbs[kb.ID] = kb
	return nil
}
func (s *memStore) GetKB(_ context.Context, id string) (*domain.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kb, ok := s.kbs[id]; ok {
		return kb, nil
	}
	return nil, os.ErrNotExist
}
func (s *memStore) ListKBs(_ context.Context, ws string) ([]*domain.KnowledgeBase, error) {
	return nil, nil
}
func (s *memStore) UpdateKBStatus(_ context.Context, id, status string) error { return nil }
func (s *memStore) UpsertDocument(_ context.Context, d *domain.KbDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[d.ID] = d
	return nil
}
func (s *memStore) GetDocument(_ context.Context, id string) (*domain.KbDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.docs[id]; ok {
		return d, nil
	}
	return nil, nil
}
func (s *memStore) ListDocuments(_ context.Context, kbID string) ([]*domain.KbDocument, error) {
	return nil, nil
}
func (s *memStore) CreateIngestJob(_ context.Context, j *domain.KbIngestJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.KbID] = append(s.jobs[j.KbID], j)
	return nil
}
func (s *memStore) UpdateIngestJob(_ context.Context, j *domain.KbIngestJob) error { return nil }
func (s *memStore) ListIngestJobs(_ context.Context, kbID string) ([]*domain.KbIngestJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[kbID], nil
}
func (s *memStore) UpsertConnector(_ context.Context, c *domain.ConnectorInstance) error {
	s.connectors[c.KbID] = append(s.connectors[c.KbID], c)
	return nil
}
func (s *memStore) ListConnectors(_ context.Context, kbID string) ([]*domain.ConnectorInstance, error) {
	return s.connectors[kbID], nil
}

func newTestService() (*Service, *memStore) {
	store := newMemStore()
	r := retriever.New(retriever.NewLocalEmbedder(256))
	return NewService(store, r, nil), store
}

func TestIngestAndSearch(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	kb, err := svc.CreateKB(ctx, CreateKBRequest{WorkspaceID: "w1", Name: "FAQ", CreatedBy: "u1"})
	if err != nil {
		t.Fatalf("create kb: %v", err)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "refund.md"),
		"# 退款政策\n\n用户在七天内可以申请退款。需要提供订单号。\n\n超过七天不支持退款。")
	mustWrite(t, filepath.Join(dir, "shipping.md"),
		"# 物流\n\n标准快递三到五个工作日送达。")

	res, err := svc.SyncMarkdownFolder(ctx, kb.ID, dir)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Ingested != 2 {
		t.Fatalf("expected 2 ingested, got %+v", res)
	}

	// 检索应命中退款文档。
	passages, err := svc.Search(ctx, kb.ID, "怎么申请退款", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(passages) == 0 {
		t.Fatal("expected search results")
	}
	// 退款相关片段应排在最前。
	if !strings.Contains(passages[0].Text, "退款") {
		t.Fatalf("expected top passage about 退款, got %q", passages[0].Text)
	}

	// ingest 任务应记录 done 阶段。
	jobs, _ := svc.ListIngestJobs(ctx, kb.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 ingest jobs, got %d", len(jobs))
	}
	for _, j := range jobs {
		if j.Stage != StageDone {
			t.Fatalf("expected job stage done, got %s (err=%v)", j.Stage, j.Error)
		}
	}
}

func TestSyncMarkdownFolderAllowedRejectsOutsideRootAndSymlinkEscape(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	kb, err := svc.CreateKB(ctx, CreateKBRequest{WorkspaceID: "w1", Name: "Allowed", CreatedBy: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	allowed := t.TempDir()
	inside := filepath.Join(allowed, "course")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(inside, "inside.md"), "inside")
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.md"), "secret")
	svc.ConfigureMarkdownAllowedRoots([]string{allowed})

	if _, err := svc.SyncMarkdownFolderAllowed(ctx, kb.ID, inside); err != nil {
		t.Fatalf("allowed root rejected: %v", err)
	}
	if _, err := svc.SyncMarkdownFolderAllowed(ctx, kb.ID, outside); err == nil {
		t.Fatal("outside root should be rejected")
	}
	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncMarkdownFolderAllowed(ctx, kb.ID, link); err == nil {
		t.Fatal("symlink escaping allowed root should be rejected")
	}
}

func TestSyncSkipsUnchanged(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	kb, _ := svc.CreateKB(ctx, CreateKBRequest{WorkspaceID: "w1", Name: "FAQ", CreatedBy: "u1"})

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "内容")

	first, err := svc.SyncMarkdownFolder(ctx, kb.ID, dir)
	if err != nil || first.Ingested != 1 {
		t.Fatalf("first sync: %+v err=%v", first, err)
	}

	// 再同步一次：hash 未变，应跳过。
	second, err := svc.SyncMarkdownFolder(ctx, kb.ID, dir)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Ingested != 0 || second.Skipped != 1 {
		t.Fatalf("expected skip on unchanged content, got %+v", second)
	}
}

func TestSyncStaticDocumentsIngestsAndSkipsUnchanged(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	base, err := svc.CreateKB(ctx, CreateKBRequest{WorkspaceID: "w1", Name: "课程规则", CreatedBy: "system"})
	if err != nil {
		t.Fatal(err)
	}
	documents := []StaticDocument{
		{SourceURI: "coursepreset://claims/coverage.md", Title: "责任规则", Content: "# 责任规则\n\n最高可赔金额需要扣除免赔额。"},
		{SourceURI: "coursepreset://claims/materials.md", Title: "材料规则", Content: "# 材料规则\n\n碰撞险需要事故报告和维修发票。"},
	}

	first, err := svc.SyncStaticDocuments(ctx, base.ID, documents)
	if err != nil {
		t.Fatalf("first static sync: %v", err)
	}
	if first.Ingested != 2 || len(store.docs) != 2 {
		t.Fatalf("first static sync = %+v, docs=%d", first, len(store.docs))
	}
	second, err := svc.SyncStaticDocuments(ctx, base.ID, documents)
	if err != nil {
		t.Fatalf("second static sync: %v", err)
	}
	if second.Ingested != 0 || second.Skipped != 2 {
		t.Fatalf("second static sync = %+v", second)
	}

	passages, err := svc.Search(ctx, base.ID, "赔款免赔额", 2)
	if err != nil || len(passages) == 0 || !strings.Contains(passages[0].Text, "免赔额") {
		t.Fatalf("static knowledge search = %+v, err=%v", passages, err)
	}
}

func TestSyncStaticDocumentsRejectsDuplicateSourceURI(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	base, _ := svc.CreateKB(ctx, CreateKBRequest{WorkspaceID: "w1", Name: "课程规则", CreatedBy: "system"})
	_, err := svc.SyncStaticDocuments(ctx, base.ID, []StaticDocument{
		{SourceURI: "coursepreset://same.md", Title: "A", Content: "A"},
		{SourceURI: "coursepreset://same.md", Title: "B", Content: "B"},
	})
	if err == nil {
		t.Fatal("duplicate static source URI was accepted")
	}
}

func TestSyncRejectsOversizedDocument(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	kb, err := svc.CreateKB(ctx, CreateKBRequest{WorkspaceID: "w1", Name: "FAQ", CreatedBy: "u1"})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "large.md"), strings.Repeat("x", maxDocumentBytes+1))
	if _, err := svc.SyncMarkdownFolder(ctx, kb.ID, dir); err == nil {
		t.Fatal("oversized document was accepted")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
