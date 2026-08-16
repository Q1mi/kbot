package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 agent.Store(含会话/消息)。
// CreateAgentVersion 自动把新版本绑到 dev 环境(对齐 memory 语义)。
type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool, q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{db: db, q: q}
}

var _ Store = (*PostgresStore)(nil)

// ---- Agent ----

func (s *PostgresStore) GetAgent(ctx context.Context, agentID string) (*domain.Agent, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	row, err := s.q.GetAgent(ctx, id)
	if err != nil {
		return nil, notFound("agent", err)
	}
	return agentFromRow(row), nil
}

func (s *PostgresStore) ListAgents(ctx context.Context, workspaceID string) ([]*domain.Agent, error) {
	rows, err := s.q.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	out := make([]*domain.Agent, 0, len(rows))
	for _, r := range rows {
		out = append(out, agentFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) CreateAgent(ctx context.Context, a *domain.Agent) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	if _, err := s.q.CreateAgent(ctx, pgstore.CreateAgentParams{
		ID:          id,
		WorkspaceID: a.WorkspaceID,
		Name:        a.Name,
		Template:    a.Template,
		Description: "",
		CreatedBy:   a.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateAgentWithVersion(ctx context.Context, a *domain.Agent, v *domain.AgentVersion) error {
	agentID, err := uuid.Parse(a.ID)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	if err := s.q.CreateAgentWithVersion(ctx, pgstore.CreateAgentWithVersionParams{
		AgentID: agentID, WorkspaceID: a.WorkspaceID, Name: a.Name, Template: a.Template,
		Description: "", AgentCreatedBy: a.CreatedBy, VersionID: versionID,
		SnapshotJson: v.SnapshotJSON, VersionCreatedBy: v.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create agent with version: %w", err)
	}
	return nil
}

func (s *PostgresStore) CreateAgentVersion(ctx context.Context, v *domain.AgentVersion) error {
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	agentID, err := uuid.Parse(v.AgentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	if err := s.q.CreateAgentVersionAndBindDev(ctx, pgstore.CreateAgentVersionAndBindDevParams{
		VersionID: id, AgentID: agentID, Version: int32(v.Version), SnapshotJson: v.SnapshotJSON, CreatedBy: v.CreatedBy,
	}); err != nil {
		return fmt.Errorf("create agent version and bind dev: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAgentVersion(ctx context.Context, versionID string) (*domain.AgentVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse version id: %w", err)
	}
	row, err := s.q.GetAgentVersion(ctx, id)
	if err != nil {
		return nil, notFound("agent version", err)
	}
	return agentVersionFromRow(row), nil
}

func (s *PostgresStore) ListAgentVersions(ctx context.Context, agentID string) ([]*domain.AgentVersion, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	rows, err := s.q.ListAgentVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	out := make([]*domain.AgentVersion, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentVersionFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) GetAgentCurrentVersion(ctx context.Context, agentID, env string) (*domain.AgentVersion, error) {
	id, err := uuid.Parse(agentID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}
	row, err := s.q.GetAgentCurrentVersion(ctx, pgstore.GetAgentCurrentVersionParams{AgentID: id, Env: env})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no version for env %s", env)
		}
		return nil, fmt.Errorf("get current version: %w", err)
	}
	return agentVersionFromRow(row), nil
}

func (s *PostgresStore) SetAgentEnvBinding(ctx context.Context, agentID, env, versionID string) error {
	aid, err := uuid.Parse(agentID)
	if err != nil {
		return fmt.Errorf("parse agent id: %w", err)
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	if err := s.q.UpsertAgentEnv(ctx, pgstore.UpsertAgentEnvParams{AgentID: aid, Env: env, VersionID: vid}); err != nil {
		return fmt.Errorf("bind agent version to %s: %w", env, err)
	}
	return nil
}

// ---- Conversation / Message ----

func (s *PostgresStore) CreateConversation(ctx context.Context, c *domain.Conversation) error {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create conversation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.CreateConversation(ctx, pgstore.CreateConversationParams{
		ID:             id,
		AgentID:        c.AgentID,
		AgentVersionID: c.AgentVersionID,
		UserID:         c.UserID,
		WorkspaceID:    c.WorkspaceID,
		Classification: orEmpty(c.Classification, "internal"),
		Status:         orEmpty(c.Status, "active"),
	}); err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}
	if c.RuntimeConfigJSON != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_runtime_configs (conversation_id,config_json)
			VALUES ($1,$2::jsonb)`, id, c.RuntimeConfigJSON); err != nil {
			return fmt.Errorf("create conversation runtime config: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create conversation: %w", err)
	}
	return nil
}

func (s *PostgresStore) ClaimConversationTurn(ctx context.Context, conversationID string, resume bool) (string, error) {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return "", fmt.Errorf("parse conversation id: %w", err)
	}
	token := uuid.New()
	tag, err := s.db.Exec(ctx, `
		UPDATE conversations
		SET status='running', turn_token=$2, turn_lease_until=now()+interval '2 minutes',
		    turn_revision=turn_revision+1, updated_at=now()
		WHERE id=$1 AND (
		  (turn_token IS NULL AND (($3=false AND status='active') OR
		                           ($3=true AND status IN ('active','awaiting_approval'))))
		  OR (status='running' AND turn_lease_until < now())
		)`, id, token, resume)
	if err != nil {
		return "", fmt.Errorf("claim conversation turn: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", fmt.Errorf("conversation turn is already running or awaiting approval")
	}
	return token.String(), nil
}

func (s *PostgresStore) RenewConversationTurn(ctx context.Context, conversationID, token string) error {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return err
	}
	tokenID, err := uuid.Parse(token)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE conversations SET turn_lease_until=now()+interval '2 minutes'
		WHERE id=$1 AND status='running' AND turn_token=$2`, id, tokenID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("conversation turn lease is unavailable")
	}
	return nil
}

func (s *PostgresStore) CommitConversationTurn(
	ctx context.Context, conversationID, token string, messages []*domain.Message, nextStatus string,
) error {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return err
	}
	tokenID, err := uuid.Parse(token)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE conversations
		SET status=$3,turn_token=NULL,turn_lease_until=NULL,updated_at=now()
		WHERE id=$1 AND status='running' AND turn_token=$2`, id, tokenID, nextStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("conversation turn lease is unavailable")
	}
	qtx := s.q.WithTx(tx)
	for _, message := range messages {
		if message == nil {
			continue
		}
		messageID, parseErr := uuid.Parse(message.ID)
		if parseErr != nil {
			return parseErr
		}
		toolCalls := []byte("[]")
		if len(message.ToolCalls) > 0 {
			toolCalls, parseErr = json.Marshal(message.ToolCalls)
			if parseErr != nil {
				return parseErr
			}
		}
		if _, err := qtx.CreateMessage(ctx, pgstore.CreateMessageParams{
			ID: messageID, ConversationID: id, Role: message.Role, Content: message.Content,
			ToolCalls: toolCalls, ToolCallID: textFromStrPtr(message.ToolCallID),
		}); err != nil {
			return fmt.Errorf("create turn message: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ReleaseConversationTurn(ctx context.Context, conversationID, token, nextStatus string) error {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return err
	}
	tokenID, err := uuid.Parse(token)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE conversations SET status=$3,turn_token=NULL,turn_lease_until=NULL,updated_at=now()
		WHERE id=$1 AND status='running' AND turn_token=$2`, id, tokenID, nextStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("conversation turn lease is unavailable")
	}
	return nil
}

// UpdateConversationRuntimeConfig 幂等更新会话运行时 Prompt 快照。
func (s *PostgresStore) UpdateConversationRuntimeConfig(ctx context.Context, conversationID, configJSON string) error {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO conversation_runtime_configs (conversation_id,config_json)
		VALUES ($1,$2::jsonb)
		ON CONFLICT (conversation_id) DO UPDATE SET config_json=EXCLUDED.config_json`, id, configJSON); err != nil {
		return fmt.Errorf("update conversation runtime config: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}
	row, err := s.q.GetConversation(ctx, id)
	if err != nil {
		return nil, notFound("conversation", err)
	}
	c := conversationFromRow(row)
	var configJSON []byte
	err = s.db.QueryRow(ctx, `
		SELECT config_json FROM conversation_runtime_configs WHERE conversation_id=$1`, id).Scan(&configJSON)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("get conversation runtime config: %w", err)
	}
	if len(configJSON) > 0 {
		c.RuntimeConfigJSON = string(configJSON)
	}
	return c, nil
}

func (s *PostgresStore) ListConversations(
	ctx context.Context, workspaceID, userID, agentID string, limit, offset int32,
) ([]*domain.Conversation, error) {
	rows, err := s.q.ListConversations(ctx, pgstore.ListConversationsParams{
		WorkspaceID: workspaceID, UserID: userID, AgentID: agentID,
		PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	out := make([]*domain.Conversation, 0, len(rows))
	for _, row := range rows {
		out = append(out, conversationFromRow(row))
	}
	return out, nil
}

func (s *PostgresStore) GetConversationMessages(ctx context.Context, conversationID string) ([]*domain.Message, error) {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, fmt.Errorf("parse conversation id: %w", err)
	}
	rows, err := s.q.ListMessages(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	out := make([]*domain.Message, 0, len(rows))
	for _, r := range rows {
		m, err := messageFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *PostgresStore) CreateMessage(ctx context.Context, m *domain.Message) error {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return fmt.Errorf("parse message id: %w", err)
	}
	convID, err := uuid.Parse(m.ConversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	toolCalls := []byte("[]")
	if len(m.ToolCalls) > 0 {
		toolCalls, err = json.Marshal(m.ToolCalls)
		if err != nil {
			return fmt.Errorf("marshal tool_calls: %w", err)
		}
	}
	if _, err := s.q.CreateMessage(ctx, pgstore.CreateMessageParams{
		ID:             id,
		ConversationID: convID,
		Role:           m.Role,
		Content:        m.Content,
		ToolCalls:      toolCalls,
		ToolCallID:     textFromStrPtr(m.ToolCallID),
	}); err != nil {
		return fmt.Errorf("create message: %w", err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, convID); err != nil {
		return fmt.Errorf("touch conversation: %w", err)
	}
	return nil
}

// ---- 行 → domain ----

func agentFromRow(r pgstore.Agent) *domain.Agent {
	return &domain.Agent{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Template:    r.Template,
		CreatedBy:   r.CreatedBy,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func agentVersionFromRow(r pgstore.AgentVersion) *domain.AgentVersion {
	return &domain.AgentVersion{
		ID:           r.ID.String(),
		AgentID:      r.AgentID.String(),
		Version:      int(r.Version),
		SnapshotJSON: r.SnapshotJson,
		CreatedBy:    r.CreatedBy,
		CreatedAt:    r.CreatedAt,
	}
}

func conversationFromRow(r pgstore.Conversation) *domain.Conversation {
	return &domain.Conversation{
		ID:             r.ID.String(),
		AgentID:        r.AgentID,
		AgentVersionID: r.AgentVersionID,
		WorkspaceID:    r.WorkspaceID,
		UserID:         r.UserID,
		Status:         r.Status,
		Classification: r.Classification,
		StartedAt:      r.StartedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func messageFromRow(r pgstore.Message) (*domain.Message, error) {
	var toolCalls []domain.ToolCall
	if len(r.ToolCalls) > 0 {
		if err := json.Unmarshal(r.ToolCalls, &toolCalls); err != nil {
			return nil, fmt.Errorf("unmarshal tool_calls: %w", err)
		}
	}
	return &domain.Message{
		ID:             r.ID.String(),
		ConversationID: r.ConversationID.String(),
		Role:           r.Role,
		Content:        r.Content,
		ToolCalls:      toolCalls,
		ToolCallID:     strPtrFromText(r.ToolCallID),
		CreatedAt:      r.CreatedAt,
	}, nil
}

// ---- 小工具 ----

func orEmpty(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

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
