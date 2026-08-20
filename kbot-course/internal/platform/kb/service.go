// Package kb 管理课堂版知识库与显式 ingest 状态机。
package kb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Q1mi/kbot/internal/connector"
)

type KnowledgeBase struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type Document struct {
	ID        string   `json:"id"`
	KBID      string   `json:"kb_id"`
	SourceURI string   `json:"source_uri"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Checksum  string   `json:"checksum"`
	Chunks    []string `json:"chunks"`
}

type IngestJob struct {
	ID         string     `json:"id"`
	KBID       string     `json:"kb_id"`
	Stage      string     `json:"stage"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	Stages     []string   `json:"stages"`
	Listed     int        `json:"listed"`
	Ingested   int        `json:"ingested"`
	Skipped    int        `json:"skipped"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Service struct {
	mu        sync.RWMutex
	bases     map[string]KnowledgeBase
	documents map[string]map[string]Document
	sequence  atomic.Uint64
}

func NewService() *Service {
	return &Service{bases: make(map[string]KnowledgeBase), documents: make(map[string]map[string]Document)}
}

func (s *Service) Create(_ context.Context, workspaceID, name string) (*KnowledgeBase, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("workspace and knowledge base name are required")
	}
	base := KnowledgeBase{ID: s.nextID("kb"), WorkspaceID: workspaceID, Name: strings.TrimSpace(name), Status: "ready", CreatedAt: time.Now().UTC()}
	s.mu.Lock()
	s.bases[base.ID] = base
	s.documents[base.ID] = make(map[string]Document)
	s.mu.Unlock()
	return &base, nil
}

func (s *Service) List(_ context.Context, workspaceID string) []KnowledgeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]KnowledgeBase, 0, len(s.bases))
	for _, base := range s.bases {
		if base.WorkspaceID == workspaceID {
			result = append(result, base)
		}
	}
	return result
}

func (s *Service) Sync(ctx context.Context, workspaceID, kbID string, source connector.Connector) (*IngestJob, error) {
	job := &IngestJob{ID: s.nextID("ingest"), KBID: kbID, Stage: "parse", Status: "running", CreatedAt: time.Now().UTC()}
	if source == nil {
		return s.fail(job, "parse", fmt.Errorf("connector is required"))
	}
	if err := s.ensureWorkspace(workspaceID, kbID); err != nil {
		return s.fail(job, "parse", err)
	}
	documents, err := source.Scan(ctx)
	if err != nil {
		return s.fail(job, "parse", err)
	}
	job.Stages = append(job.Stages, "parse")
	job.Listed = len(documents)
	if len(documents) == 0 {
		return s.fail(job, "chunk", fmt.Errorf("connector returned no markdown documents"))
	}
	indexed := make(map[string]Document, len(documents))
	job.Stage = "chunk"
	for _, document := range documents {
		chunks := chunkText(document.Content, 500, 50)
		if len(chunks) == 0 {
			return s.fail(job, "chunk", fmt.Errorf("document %s has no indexable content", document.SourceURI))
		}
		indexed[document.SourceURI] = Document{ID: s.nextID("doc"), KBID: kbID, SourceURI: document.SourceURI, Title: document.Title, Content: document.Content, Checksum: document.Checksum, Chunks: chunks}
	}
	job.Stages = append(job.Stages, "chunk")
	job.Stage = "embed"
	job.Stages = append(job.Stages, "embed")
	job.Stage = "index"
	s.mu.Lock()
	for sourceURI, document := range indexed {
		if previous, ok := s.documents[kbID][sourceURI]; ok && previous.Checksum == document.Checksum {
			document.ID = previous.ID
			job.Skipped++
		} else {
			job.Ingested++
		}
		s.documents[kbID][sourceURI] = document
	}
	s.mu.Unlock()
	job.Stages = append(job.Stages, "index")
	now := time.Now().UTC()
	job.Stage, job.Status, job.FinishedAt = "done", "succeeded", &now
	job.Stages = append(job.Stages, "done")
	return job, nil
}

func (s *Service) Documents(_ context.Context, workspaceID, kbID string) ([]Document, error) {
	if err := s.ensureWorkspace(workspaceID, kbID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Document, 0, len(s.documents[kbID]))
	for _, document := range s.documents[kbID] {
		document.Chunks = append([]string(nil), document.Chunks...)
		result = append(result, document)
	}
	return result, nil
}

func (s *Service) ensureWorkspace(workspaceID, kbID string) error {
	s.mu.RLock()
	base, ok := s.bases[kbID]
	s.mu.RUnlock()
	if !ok || base.WorkspaceID != workspaceID {
		return fmt.Errorf("knowledge base %s not found", kbID)
	}
	return nil
}

func (s *Service) fail(job *IngestJob, stage string, err error) (*IngestJob, error) {
	now := time.Now().UTC()
	job.Stage, job.Status, job.Error, job.FinishedAt = stage, "failed", err.Error(), &now
	return job, err
}

func (s *Service) nextID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, s.sequence.Add(1))
}

func chunkText(content string, size, overlap int) []string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) == 0 || size <= 0 || overlap < 0 || overlap >= size {
		return nil
	}
	var chunks []string
	for start := 0; start < len(runes); start += size - overlap {
		end := min(start+size, len(runes))
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
