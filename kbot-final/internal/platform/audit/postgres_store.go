package audit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Q1mi/kbot/internal/domain"
	pgstore "github.com/Q1mi/kbot/internal/infrastructure/postgres/sqlc"
)

// PostgresStore 用 sqlc 实现 audit.Store(写入 audit_logs 月分区父表)。
// id 是 BIGSERIAL(domain.AuditLog.ID 为字符串,回读时 strconv);插入忽略 domain.ID 让序列生成。
type PostgresStore struct {
	q *pgstore.Queries
}

func NewPostgresStore(q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{q: q}
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) BatchInsert(ctx context.Context, logs []*domain.AuditLog) error {
	for _, l := range logs {
		created := l.CreatedAt
		if created.IsZero() {
			created = time.Now()
		}
		if err := s.q.InsertAuditLog(ctx, pgstore.InsertAuditLogParams{
			WorkspaceID:  l.WorkspaceID,
			Actor:        l.Actor,
			Action:       l.Action,
			ResourceType: l.ResourceType,
			ResourceID:   l.ResourceID,
			BeforeJson:   textFromStrPtr(l.BeforeJSON),
			AfterJson:    textFromStrPtr(l.AfterJSON),
			Ip:           textFromStrPtr(l.IP),
			Ua:           textFromStrPtr(l.UserAgent),
			CreatedAt:    created,
		}); err != nil {
			return fmt.Errorf("insert audit log: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) Query(ctx context.Context, f QueryFilter) ([]*domain.AuditLog, error) {
	rows, err := s.q.QueryAuditLogs(ctx, pgstore.QueryAuditLogsParams{
		WorkspaceID:    f.WorkspaceID,
		Actor:          f.Actor,
		ResourceType:   f.ResourceType,
		ConversationID: f.ConversationID,
		Lim:            int32(f.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}
	out := make([]*domain.AuditLog, 0, len(rows))
	for _, r := range rows {
		out = append(out, auditFromRow(r))
	}
	return out, nil
}

func (s *PostgresStore) InsertSkillTrigger(
	ctx context.Context, workspaceID, conversationID, skillVersionID, skillName, source string,
) error {
	conversationUUID, err := uuid.Parse(conversationID)
	if err != nil {
		return fmt.Errorf("parse conversation id: %w", err)
	}
	var versionUUID pgtype.UUID
	if skillVersionID != "" {
		parsed, parseErr := uuid.Parse(skillVersionID)
		if parseErr != nil {
			return fmt.Errorf("parse skill version id: %w", parseErr)
		}
		versionUUID = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	if err := s.q.InsertSkillTrigger(ctx, pgstore.InsertSkillTriggerParams{
		ID:             uuid.New(),
		WorkspaceID:    workspaceID,
		ConversationID: conversationUUID,
		SkillVersionID: versionUUID,
		SkillName:      skillName,
		Source:         source,
	}); err != nil {
		return fmt.Errorf("insert skill trigger: %w", err)
	}
	return nil
}

func auditFromRow(r pgstore.AuditLog) *domain.AuditLog {
	return &domain.AuditLog{
		ID:           strconv.FormatInt(r.ID, 10),
		WorkspaceID:  r.WorkspaceID,
		Actor:        r.Actor,
		Action:       r.Action,
		ResourceType: r.ResourceType,
		ResourceID:   r.ResourceID,
		BeforeJSON:   strPtrFromText(r.BeforeJson),
		AfterJSON:    strPtrFromText(r.AfterJson),
		IP:           strPtrFromText(r.Ip),
		UserAgent:    strPtrFromText(r.Ua),
		CreatedAt:    r.CreatedAt,
	}
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
