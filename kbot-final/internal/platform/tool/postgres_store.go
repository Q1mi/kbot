package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 tool.Store。工具用「当前版本=最新版本号」语义(无 env 指针)。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

// ---- Tool ----

func (s *PostgresStore) GetTool(ctx context.Context, toolID string) (*domain.Tool, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, fmt.Errorf("parse tool id: %w", err)
	}
	row, err := s.q.GetTool(ctx, id)
	if err != nil {
		return nil, notFound("tool", err)
	}
	return toolFromRow(row), nil
}

func (s *PostgresStore) CreateTool(ctx context.Context, t *domain.Tool) error {
	id, err := uuid.Parse(t.ID)
	if err != nil {
		return fmt.Errorf("parse tool id: %w", err)
	}
	if _, err := s.q.CreateTool(ctx, pgstore.CreateToolParams{
		ID:                id,
		WorkspaceID:       t.WorkspaceID,
		Name:              t.Name,
		Description:       t.Description,
		SourceType:        t.SourceType,
		Sensitive:         t.Sensitive,
		ClassificationMax: "internal",
		CreatedBy:         t.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create tool: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateToolWithVersion(ctx context.Context, t *domain.Tool, v *domain.ToolVersion) error {
	toolID, err := uuid.Parse(t.ID)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	if err := s.q.CreateToolWithVersion(ctx, pgstore.CreateToolWithVersionParams{
		ToolID: toolID, WorkspaceID: t.WorkspaceID, Name: t.Name, Description: t.Description,
		SourceType: t.SourceType, Sensitive: t.Sensitive, ClassificationMax: "internal", ToolCreatedBy: t.CreatedBy,
		VersionID: versionID, SchemaJson: v.SchemaJSON, EndpointConfig: v.EndpointConfig,
		AuthConfig: v.AuthConfig, AuthConfigEncrypted: v.AuthConfigEncrypted, RetryPolicy: v.RetryPolicy,
		VersionStatus: v.Status, VersionCreatedBy: v.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create tool with version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListTools(ctx context.Context, workspaceID string) ([]*domain.Tool, error) {
	rows, err := s.q.ListTools(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	out := make([]*domain.Tool, 0, len(rows))
	for _, r := range rows {
		out = append(out, toolFromRow(r))
	}
	return out, nil
}

// ---- ToolVersion ----

func (s *PostgresStore) GetToolVersion(ctx context.Context, versionID string) (*domain.ToolVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse version id: %w", err)
	}
	row, err := s.q.GetToolVersion(ctx, id)
	if err != nil {
		return nil, notFound("tool version", err)
	}
	return toolVersionFromRow(row), nil
}

func (s *PostgresStore) CreateToolVersion(ctx context.Context, v *domain.ToolVersion) error {
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	toolID, err := uuid.Parse(v.ToolID)
	if err != nil {
		return fmt.Errorf("parse tool id: %w", err)
	}
	if _, err := s.q.CreateToolVersion(ctx, pgstore.CreateToolVersionParams{
		ID:                  id,
		ToolID:              toolID,
		Version:             int32(v.Version),
		SchemaJson:          v.SchemaJSON,
		EndpointConfig:      v.EndpointConfig,
		AuthConfig:          v.AuthConfig,
		AuthConfigEncrypted: v.AuthConfigEncrypted,
		RetryPolicy:         v.RetryPolicy,
		Status:              v.Status,
		CreatedBy:           v.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create tool version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListLegacyToolAuthVersions(ctx context.Context) ([]*domain.ToolVersion, error) {
	rows, err := s.q.ListLegacyToolAuthVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list legacy tool auth versions: %w", err)
	}
	out := make([]*domain.ToolVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, toolVersionFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) EncryptToolVersionAuth(ctx context.Context, versionID string, ciphertext []byte) error {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse tool version id: %w", err)
	}
	return s.q.EncryptToolVersionAuth(ctx, pgstore.EncryptToolVersionAuthParams{ID: id, AuthConfigEncrypted: ciphertext})
}

func (s *PostgresStore) CreateInvocation(ctx context.Context, invocation *domain.ToolInvocation) error {
	id, err := uuid.Parse(invocation.ID)
	if err != nil {
		return err
	}
	args := jsonValue(invocation.Args)
	result := jsonValue(invocation.Result)
	return s.q.CreateToolInvocation(ctx, pgstore.CreateToolInvocationParams{
		ID: id, WorkspaceID: invocation.WorkspaceID, ConversationID: optionalUUID(invocation.ConversationID),
		ToolCallID: invocation.ToolCallID, ToolVersionID: optionalUUID(invocation.ToolVersionID),
		Args: args, Result: result, Status: invocation.Status, LatencyMs: int32(invocation.LatencyMS), Error: invocation.Error,
	})
}

func (s *PostgresStore) CompleteInvocation(ctx context.Context, invocationID, result, status string, latencyMS int, errorMessage string) error {
	id, err := uuid.Parse(invocationID)
	if err != nil {
		return err
	}
	rows, err := s.q.CompleteToolInvocation(ctx, pgstore.CompleteToolInvocationParams{
		ID: id, Result: jsonValue(result), Status: status, LatencyMs: int32(latencyMS), Error: errorMessage,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("tool invocation is not running")
	}
	return nil
}

func (s *PostgresStore) CreateSandboxExecution(ctx context.Context, execution *domain.SandboxExecution) error {
	id, err := uuid.Parse(execution.ID)
	if err != nil {
		return err
	}
	return s.q.CreateSandboxExecution(ctx, pgstore.CreateSandboxExecutionParams{
		ID: id, WorkspaceID: execution.WorkspaceID,
		ConversationID: optionalUUID(execution.ConversationID), InvocationID: optionalUUID(execution.InvocationID),
		ToolVersionID: optionalUUID(execution.ToolVersionID), ToolCallID: execution.ToolCallID,
		ExecutionID: execution.ExecutionID, Language: execution.Language, ContainerID: execution.ContainerID,
		ExitCode: pgtype.Int4{Int32: int32(execution.ExitCode), Valid: true},
		Stdout:   execution.Stdout, Stderr: execution.Stderr, DurationMs: execution.DurationMS,
		TimedOut: execution.TimedOut, OutputTruncated: execution.OutputTruncated, Status: execution.Status,
	})
}

func jsonValue(value string) []byte {
	if json.Valid([]byte(value)) {
		return []byte(value)
	}
	encoded, _ := json.Marshal(map[string]string{"value": value})
	return encoded
}

func optionalUUID(value string) pgtype.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func (s *PostgresStore) GetToolCurrentVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, fmt.Errorf("parse tool id: %w", err)
	}
	row, err := s.q.GetToolCurrentVersion(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("tool has no version")
		}
		return nil, fmt.Errorf("get current version: %w", err)
	}
	return toolVersionFromRow(row), nil
}

func (s *PostgresStore) GetToolLatestPublishedVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, fmt.Errorf("parse tool id: %w", err)
	}
	row, err := s.q.GetToolLatestPublishedVersion(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("tool has no published version")
		}
		return nil, fmt.Errorf("get latest published version: %w", err)
	}
	return toolVersionFromRow(row), nil
}

func (s *PostgresStore) ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, fmt.Errorf("parse tool id: %w", err)
	}
	rows, err := s.q.ListToolVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list tool versions: %w", err)
	}
	out := make([]*domain.ToolVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, toolVersionFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) UpdateToolVersionStatus(ctx context.Context, versionID, status string) error {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	if err := s.q.UpdateToolVersionStatus(ctx, pgstore.UpdateToolVersionStatusParams{ID: id, Status: status}); err != nil {
		return fmt.Errorf("update version status: %w", err)
	}
	return nil
}

// ---- ToolTestRun ----

func (s *PostgresStore) CreateTestRun(ctx context.Context, tr *domain.ToolTestRun) error {
	id, err := uuid.Parse(tr.ID)
	if err != nil {
		return fmt.Errorf("parse test run id: %w", err)
	}
	toolID, err := uuid.Parse(tr.ToolID)
	if err != nil {
		return fmt.Errorf("parse tool id: %w", err)
	}
	versionID, err := uuid.Parse(tr.ToolVersionID)
	if err != nil {
		return fmt.Errorf("parse tool version id: %w", err)
	}
	if _, err := s.q.CreateToolTestRun(ctx, pgstore.CreateToolTestRunParams{
		ID: id, ToolID: toolID, ToolVersionID: versionID,
		Input: tr.Input, Output: tr.Output, Status: tr.Status,
		LatencyMs: int32(tr.LatencyMs), Error: textFromStrPtr(tr.Error),
	}); err != nil {
		return fmt.Errorf("create test run: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetToolLastSuccessfulTestRunForVersion(ctx context.Context, versionID string) (*domain.ToolTestRun, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse tool version id: %w", err)
	}
	row, err := s.q.GetToolLastSuccessfulTestRunForVersion(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no successful test run for tool version")
		}
		return nil, fmt.Errorf("get last version test run: %w", err)
	}
	return testRunFromRow(row), nil
}

func (s *PostgresStore) GetToolLastSuccessfulTestRun(ctx context.Context, toolID string) (*domain.ToolTestRun, error) {
	id, err := uuid.Parse(toolID)
	if err != nil {
		return nil, fmt.Errorf("parse tool id: %w", err)
	}
	row, err := s.q.GetToolLastSuccessfulTestRun(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no successful test run")
		}
		return nil, fmt.Errorf("get last test run: %w", err)
	}
	return testRunFromRow(row), nil
}

// ---- 行 → domain ----

func toolFromRow(r pgstore.Tool) *domain.Tool {
	return &domain.Tool{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		SourceType:  r.SourceType,
		Description: r.Description,
		Sensitive:   r.Sensitive,
		CreatedBy:   r.CreatedBy,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toolVersionFromRow(r pgstore.ToolVersion) *domain.ToolVersion {
	return &domain.ToolVersion{
		ID:                  r.ID.String(),
		ToolID:              r.ToolID.String(),
		Version:             int(r.Version),
		SchemaJSON:          r.SchemaJson,
		EndpointConfig:      r.EndpointConfig,
		AuthConfig:          r.AuthConfig,
		AuthConfigEncrypted: append([]byte(nil), r.AuthConfigEncrypted...),
		HasAuth:             len(r.AuthConfigEncrypted) > 0 || strings.TrimSpace(r.AuthConfig) != "" && strings.TrimSpace(r.AuthConfig) != "{}",
		RetryPolicy:         r.RetryPolicy,
		Status:              r.Status,
		CreatedBy:           r.CreatedBy,
		CreatedAt:           r.CreatedAt,
	}
}

func testRunFromRow(r pgstore.ToolTestRun) *domain.ToolTestRun {
	return &domain.ToolTestRun{
		ID: r.ID.String(), ToolID: r.ToolID.String(), ToolVersionID: r.ToolVersionID.String(),
		Input: r.Input, Output: r.Output, Status: r.Status,
		LatencyMs: int(r.LatencyMs), Error: strPtrFromText(r.Error), CreatedAt: r.CreatedAt,
	}
}

// ---- 小工具 ----

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}

func textFromStrPtr(p *string) pgtype.Text {
	if p == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *p, Valid: true}
}

func strPtrFromText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}
