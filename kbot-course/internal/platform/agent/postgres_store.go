package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type postgresStore struct{ pool *pgxpool.Pool }

func NewPostgresService(pool *pgxpool.Pool) *Service {
	service := NewService()
	service.postgres = &postgresStore{pool: pool}
	return service
}

func (s *postgresStore) createAgent(ctx context.Context, workspaceID, name, template string) (*Agent, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("workspace and agent name are required")
	}
	var result Agent
	err := s.pool.QueryRow(ctx, `INSERT INTO agents (id,workspace_id,name,template)
		VALUES (gen_random_uuid()::text,$1,$2,$3) RETURNING id,workspace_id,name,template,created_at`,
		workspaceID, name, template).Scan(&result.ID, &result.WorkspaceID, &result.Name, &result.Template, &result.CreatedAt)
	return &result, err
}

func (s *postgresStore) listAgents(ctx context.Context, workspaceID string) ([]Agent, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,template,created_at FROM agents WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Agent, 0)
	for rows.Next() {
		var item Agent
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Template, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *postgresStore) getAgent(ctx context.Context, workspaceID, agentID string) (Agent, error) {
	var item Agent
	err := s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,template,created_at FROM agents WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID).
		Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Template, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, fmt.Errorf("agent %s not found", agentID)
	}
	return item, err
}

func (s *postgresStore) publish(ctx context.Context, version domain.AgentVersion, snapshot engine.AgentSnapshot) error {
	if version.Version <= 0 {
		return fmt.Errorf("agent version number must be positive")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal agent snapshot: %w", err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO agent_versions (id,workspace_id,agent_id,version,snapshot,created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`, version.ID, version.WorkspaceID, version.AgentID, version.Version, raw, version.CreatedAt)
	return err
}

func (s *postgresStore) promote(ctx context.Context, workspaceID, agentID, environment, versionID string) error {
	command, err := s.pool.Exec(ctx, `INSERT INTO agent_promotions (workspace_id,agent_id,environment,agent_version_id)
		SELECT workspace_id,agent_id,$3,id FROM agent_versions WHERE id=$4 AND workspace_id=$1 AND agent_id=$2
		ON CONFLICT (workspace_id,agent_id,environment) DO UPDATE SET agent_version_id=EXCLUDED.agent_version_id,promoted_at=now()`,
		workspaceID, agentID, environment, versionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("agent version %s not found", versionID)
	}
	return nil
}

func (s *postgresStore) resolveVersion(ctx context.Context, workspaceID, agentID, environment string) (string, error) {
	var versionID string
	err := s.pool.QueryRow(ctx, `SELECT agent_version_id FROM agent_promotions WHERE workspace_id=$1 AND agent_id=$2 AND environment=$3`, workspaceID, agentID, environment).Scan(&versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("agent %s has no version promoted to %s", agentID, environment)
	}
	return versionID, err
}

func (s *postgresStore) createConversation(ctx context.Context, workspaceID, agentID, environment, userID string) (*domain.Conversation, error) {
	var conversation domain.Conversation
	err := s.pool.QueryRow(ctx, `INSERT INTO conversations (id,workspace_id,agent_id,agent_version_id,user_id)
		SELECT gen_random_uuid()::text,$1,$2,p.agent_version_id,$4 FROM agent_promotions p
		WHERE p.workspace_id=$1 AND p.agent_id=$2 AND p.environment=$3
		RETURNING id,workspace_id,agent_id,agent_version_id,user_id,created_at`,
		workspaceID, agentID, environment, userID).Scan(
		&conversation.ID, &conversation.WorkspaceID, &conversation.AgentID,
		&conversation.AgentVersionID, &conversation.UserID, &conversation.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("agent %s has no version promoted to %s", agentID, environment)
	}
	return &conversation, err
}

func (s *postgresStore) createConversationForVersion(ctx context.Context, workspaceID, agentID, versionID, userID string) (*domain.Conversation, error) {
	var conversation domain.Conversation
	err := s.pool.QueryRow(ctx, `INSERT INTO conversations (id,workspace_id,agent_id,agent_version_id,user_id)
		SELECT gen_random_uuid()::text,$1,$2,v.id,$4 FROM agent_versions v
		WHERE v.workspace_id=$1 AND v.agent_id=$2 AND v.id=$3
		RETURNING id,workspace_id,agent_id,agent_version_id,user_id,created_at`,
		workspaceID, agentID, versionID, userID).Scan(
		&conversation.ID, &conversation.WorkspaceID, &conversation.AgentID,
		&conversation.AgentVersionID, &conversation.UserID, &conversation.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("agent version %s not found", versionID)
	}
	return &conversation, err
}

func (s *postgresStore) snapshot(ctx context.Context, workspaceID, versionID string) (*engine.AgentSnapshot, error) {
	var raw []byte
	var err error
	if workspaceID == "" {
		err = s.pool.QueryRow(ctx, `SELECT snapshot FROM agent_versions WHERE id=$1`, versionID).Scan(&raw)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT snapshot FROM agent_versions WHERE id=$1 AND workspace_id=$2`, versionID, workspaceID).Scan(&raw)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("agent snapshot %s not found", versionID)
	}
	if err != nil {
		return nil, err
	}
	var snapshot engine.AgentSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	return &snapshot, nil
}

func (s *postgresStore) loadConversation(ctx context.Context, conversationID string) (*domain.Conversation, error) {
	var conversation domain.Conversation
	err := s.pool.QueryRow(ctx, `SELECT id,workspace_id,agent_id,agent_version_id,user_id,created_at FROM conversations WHERE id=$1`, conversationID).
		Scan(&conversation.ID, &conversation.WorkspaceID, &conversation.AgentID, &conversation.AgentVersionID, &conversation.UserID, &conversation.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("conversation %s not found", conversationID)
	}
	return &conversation, err
}

func (s *postgresStore) listVersions(ctx context.Context, workspaceID, agentID string) ([]domain.AgentVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,agent_id,workspace_id,version,snapshot->>'SystemPrompt',created_at
		FROM agent_versions WHERE workspace_id=$1 AND agent_id=$2 ORDER BY version`, workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.AgentVersion, 0)
	for rows.Next() {
		var version domain.AgentVersion
		if err := rows.Scan(&version.ID, &version.AgentID, &version.WorkspaceID, &version.Version, &version.SystemPrompt, &version.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, version)
	}
	return result, rows.Err()
}

func (s *postgresStore) listConversations(ctx context.Context, workspaceID, agentID string) ([]domain.Conversation, error) {
	query := `SELECT id,workspace_id,agent_id,agent_version_id,user_id,created_at FROM conversations WHERE workspace_id=$1`
	arguments := []any{workspaceID}
	if agentID != "" {
		query += ` AND agent_id=$2`
		arguments = append(arguments, agentID)
	}
	query += ` ORDER BY created_at,id`
	rows, err := s.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Conversation, 0)
	for rows.Next() {
		var conversation domain.Conversation
		if err := rows.Scan(&conversation.ID, &conversation.WorkspaceID, &conversation.AgentID, &conversation.AgentVersionID, &conversation.UserID, &conversation.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, conversation)
	}
	return result, rows.Err()
}

func (s *postgresStore) listMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.id,m.conversation_id,m.role,m.content,m.created_at FROM messages m
		JOIN conversations c ON c.id=m.conversation_id WHERE c.workspace_id=$1 AND c.id=$2 ORDER BY m.created_at,m.id`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Message, 0)
	for rows.Next() {
		var message domain.Message
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.Role, &message.Content, &message.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("conversation %s not found", conversationID)
		}
	}
	return result, nil
}

func (s *postgresStore) appendMessage(ctx context.Context, workspaceID, conversationID, role, content string) error {
	command, err := s.pool.Exec(ctx, `INSERT INTO messages (id,conversation_id,role,content)
		SELECT gen_random_uuid()::text,id,$3,$4 FROM conversations WHERE id=$2 AND workspace_id=$1`, workspaceID, conversationID, role, content)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("conversation %s not found", conversationID)
	}
	return nil
}
