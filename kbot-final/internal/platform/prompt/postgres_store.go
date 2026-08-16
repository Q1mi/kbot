package prompt

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

// PostgresStore 用 sqlc 实现 prompt.Store。所有 ID 列是 UUID,uuid.Parse 接受
// 32-hex 与 canonical 两种形式并归一,故无需回写 canonical ID。
type PostgresStore struct {
	db *pgxpool.Pool
	q  *pgstore.Queries
}

func NewPostgresStore(db *pgxpool.Pool, q *pgstore.Queries) *PostgresStore {
	return &PostgresStore{db: db, q: q}
}

var _ Store = (*PostgresStore)(nil)

// ---- Prompt ----

func (s *PostgresStore) CreatePrompt(ctx context.Context, p *domain.Prompt) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	row, err := s.q.CreatePrompt(ctx, pgstore.CreatePromptParams{
		ID:          id,
		WorkspaceID: p.WorkspaceID,
		Name:        p.Name,
		Category:    p.Category,
		CreatedBy:   p.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("create prompt: %w", err)
	}
	// 写回 canonical ID + DB 时间戳,保证 Service 后续用 p.ID 与读回的 version.PromptID 同形(否则 Promote 的归属校验会因 32-hex vs canonical 失败)。
	*p = *promptFromRow(row)
	return nil
}

func (s *PostgresStore) DeletePrompt(ctx context.Context, promptID string) error {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM prompts WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete prompt: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPrompt(ctx context.Context, promptID string) (*domain.Prompt, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	row, err := s.q.GetPrompt(ctx, id)
	if err != nil {
		return nil, notFound("prompt", err)
	}
	return promptFromRow(row), nil
}

func (s *PostgresStore) ListPrompts(ctx context.Context, workspaceID string) ([]*domain.Prompt, error) {
	rows, err := s.q.ListPrompts(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	out := make([]*domain.Prompt, 0, len(rows))
	for _, r := range rows {
		out = append(out, promptFromRow(r))
	}
	return out, nil
}

// ---- PromptVersion(immutable) ----

func (s *PostgresStore) CreatePromptVersion(ctx context.Context, v *domain.PromptVersion) error {
	// CreatePromptVersion 返回的基础表行不包含版本化模型配置。先保存调用方
	// 传入的配置，避免用数据库 canonical row 回填实体时把它们清空。
	modelProfileVersionID := v.ModelProfileVersionID
	generationConfig := v.GenerationConfig
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	promptID, err := uuid.Parse(v.PromptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin prompt version: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)
	row, err := q.CreatePromptVersion(ctx, pgstore.CreatePromptVersionParams{
		ID:              id,
		PromptID:        promptID,
		Version:         int32(v.Version),
		Template:        v.Template,
		VariablesSchema: v.VariablesSchema,
		Hash:            v.Hash,
		TokenEstimate:   int32(v.TokenEstimate),
		CreatedBy:       v.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("create prompt version: %w", err)
	}
	*v = *versionFromRow(row) // 写回 canonical ID/PromptID,与 p.ID 同形
	v.ModelProfileVersionID = modelProfileVersionID
	v.GenerationConfig = generationConfig
	if modelProfileVersionID != "" || !emptyGenerationConfig(generationConfig) {
		var profileID any
		if modelProfileVersionID != "" {
			parsed, err := uuid.Parse(modelProfileVersionID)
			if err != nil {
				return fmt.Errorf("parse model profile version id: %w", err)
			}
			profileID = parsed
		}
		configJSON, err := json.Marshal(generationConfig)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO prompt_version_configs
			  (prompt_version_id,model_profile_version_id,generation_config)
			VALUES ($1,$2,$3)`, row.ID, profileID, configJSON); err != nil {
			return fmt.Errorf("create prompt version config: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit prompt version: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPromptVersion(ctx context.Context, versionID string) (*domain.PromptVersion, error) {
	id, err := uuid.Parse(versionID)
	if err != nil {
		return nil, fmt.Errorf("parse version id: %w", err)
	}
	row, err := s.q.GetPromptVersion(ctx, id)
	if err != nil {
		return nil, notFound("prompt version", err)
	}
	v := versionFromRow(row)
	if err := s.loadVersionConfig(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *PostgresStore) GetPromptVersionByNumber(ctx context.Context, promptID string, version int) (*domain.PromptVersion, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	row, err := s.q.GetPromptVersionByNumber(ctx, pgstore.GetPromptVersionByNumberParams{
		PromptID: id,
		Version:  int32(version),
	})
	if err != nil {
		return nil, notFound("prompt version", err)
	}
	v := versionFromRow(row)
	if err := s.loadVersionConfig(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *PostgresStore) ListPromptVersions(ctx context.Context, promptID string) ([]*domain.PromptVersion, error) {
	id, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	rows, err := s.q.ListPromptVersions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list prompt versions: %w", err)
	}
	out := make([]*domain.PromptVersion, 0, len(rows))
	for _, r := range rows {
		v := versionFromRow(r)
		if err := s.loadVersionConfig(ctx, v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ---- Env 指针 ----

func (s *PostgresStore) SetEnvBinding(ctx context.Context, promptID, env, versionID string) error {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	vid, err := uuid.Parse(versionID)
	if err != nil {
		return fmt.Errorf("parse version id: %w", err)
	}
	if err := s.q.UpsertPromptEnv(ctx, pgstore.UpsertPromptEnvParams{
		PromptID:  pid,
		Env:       env,
		VersionID: vid,
	}); err != nil {
		return fmt.Errorf("upsert prompt env: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetEnvBinding(ctx context.Context, promptID, env string) (string, error) {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return "", fmt.Errorf("parse prompt id: %w", err)
	}
	vid, err := s.q.GetPromptEnv(ctx, pgstore.GetPromptEnvParams{PromptID: pid, Env: env})
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("no version bound for env %s", env)
		}
		return "", fmt.Errorf("get prompt env: %w", err)
	}
	return vid.String(), nil
}

// ---- A/B 实验 ----

func (s *PostgresStore) GetActiveExperiment(ctx context.Context, promptID, env string) (*domain.PromptExperiment, error) {
	pid, err := uuid.Parse(promptID)
	if err != nil {
		return nil, fmt.Errorf("parse prompt id: %w", err)
	}
	row := s.db.QueryRow(ctx, `
		SELECT id::text,prompt_id::text,env,variants,status,started_at,
		       baseline_version_id::text,candidate_version_id::text,traffic_percent,completed_at
		FROM prompt_experiments
		WHERE prompt_id=$1 AND env=$2 AND status='active'
		ORDER BY started_at DESC LIMIT 1`, pid, env)
	var (
		exp                 domain.PromptExperiment
		variants            []byte
		started             pgtype.Timestamptz
		baseline, candidate pgtype.Text
		completed           pgtype.Timestamptz
	)
	err = row.Scan(&exp.ID, &exp.PromptID, &exp.Env, &variants, &exp.Status, &started,
		&baseline, &candidate, &exp.TrafficPercent, &completed)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // 无实验:与 memory 版语义一致
		}
		return nil, fmt.Errorf("get experiment: %w", err)
	}
	if exp.Status != "active" {
		return nil, nil
	}
	if err := json.Unmarshal(variants, &exp.Variants); err != nil {
		return nil, err
	}
	if started.Valid {
		exp.StartedAt = started.Time
	}
	if baseline.Valid {
		exp.BaselineVersionID = baseline.String
	}
	if candidate.Valid {
		exp.CandidateVersionID = candidate.String
	}
	if completed.Valid {
		t := completed.Time
		exp.CompletedAt = &t
	}
	return &exp, nil
}

func (s *PostgresStore) UpsertExperiment(ctx context.Context, exp *domain.PromptExperiment) error {
	id, err := uuid.Parse(exp.ID)
	if err != nil {
		return fmt.Errorf("parse experiment id: %w", err)
	}
	pid, err := uuid.Parse(exp.PromptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	variants, err := json.Marshal(exp.Variants)
	if err != nil {
		return fmt.Errorf("marshal variants: %w", err)
	}
	var baseline, candidate any
	if exp.BaselineVersionID != "" {
		baseline, err = uuid.Parse(exp.BaselineVersionID)
		if err != nil {
			return err
		}
	}
	if exp.CandidateVersionID != "" {
		candidate, err = uuid.Parse(exp.CandidateVersionID)
		if err != nil {
			return err
		}
	}
	var completed any
	if exp.CompletedAt != nil {
		completed = *exp.CompletedAt
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO prompt_experiments
		  (id,prompt_id,env,variants,status,started_at,baseline_version_id,
		   candidate_version_id,traffic_percent,updated_at,completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now(),$10)
		ON CONFLICT (id) DO UPDATE SET
		  variants=EXCLUDED.variants,status=EXCLUDED.status,
		  started_at=EXCLUDED.started_at,baseline_version_id=EXCLUDED.baseline_version_id,
		  candidate_version_id=EXCLUDED.candidate_version_id,
		  traffic_percent=EXCLUDED.traffic_percent,updated_at=now(),
		  completed_at=EXCLUDED.completed_at`,
		id, pid, exp.Env, variants, exp.Status, exp.StartedAt, baseline, candidate,
		exp.TrafficPercent, completed); err != nil {
		return fmt.Errorf("upsert experiment: %w", err)
	}
	return nil
}

func (s *PostgresStore) AppendRolloutEvent(ctx context.Context, event *domain.PromptRolloutEvent) error {
	id, err := uuid.Parse(event.ID)
	if err != nil {
		return err
	}
	expID, err := uuid.Parse(event.ExperimentID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO prompt_rollout_events
		  (id,experiment_id,action,from_traffic,to_traffic,actor,detail,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, expID, event.Action, event.FromTraffic, event.ToTraffic, event.Actor, event.Detail, event.CreatedAt)
	return err
}

// CompleteRollout 原子地把候选版本切为环境基线、结束实验并记录审计事件。
// 三者必须同成同败，避免运行配置已经转全但控制面仍显示灰度中的不一致。
func (s *PostgresStore) CompleteRollout(ctx context.Context, exp *domain.PromptExperiment, event *domain.PromptRolloutEvent) error {
	promptID, err := uuid.Parse(exp.PromptID)
	if err != nil {
		return fmt.Errorf("parse prompt id: %w", err)
	}
	candidateID, err := uuid.Parse(exp.CandidateVersionID)
	if err != nil {
		return fmt.Errorf("parse candidate version id: %w", err)
	}
	experimentID, err := uuid.Parse(exp.ID)
	if err != nil {
		return fmt.Errorf("parse experiment id: %w", err)
	}
	eventID, err := uuid.Parse(event.ID)
	if err != nil {
		return fmt.Errorf("parse rollout event id: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rollout completion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		INSERT INTO prompt_envs (prompt_id,env,version_id,updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (prompt_id,env) DO UPDATE SET
		  version_id=EXCLUDED.version_id,updated_at=now()`,
		promptID, exp.Env, candidateID); err != nil {
		return fmt.Errorf("promote rollout candidate: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE prompt_experiments
		SET status='completed',traffic_percent=100,updated_at=now(),completed_at=$2
		WHERE id=$1 AND status='active'`, experimentID, exp.CompletedAt)
	if err != nil {
		return fmt.Errorf("complete experiment: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("active rollout no longer exists")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO prompt_rollout_events
		  (id,experiment_id,action,from_traffic,to_traffic,actor,detail,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		eventID, experimentID, event.Action, event.FromTraffic, event.ToTraffic,
		event.Actor, event.Detail, event.CreatedAt); err != nil {
		return fmt.Errorf("append rollout event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rollout completion: %w", err)
	}
	return nil
}

func (s *PostgresStore) loadVersionConfig(ctx context.Context, v *domain.PromptVersion) error {
	var profileID pgtype.UUID
	var configJSON []byte
	err := s.db.QueryRow(ctx, `
		SELECT model_profile_version_id,generation_config
		FROM prompt_version_configs WHERE prompt_version_id=$1`, uuid.MustParse(v.ID)).
		Scan(&profileID, &configJSON)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get prompt version config: %w", err)
	}
	if profileID.Valid {
		v.ModelProfileVersionID = uuid.UUID(profileID.Bytes).String()
	}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &v.GenerationConfig); err != nil {
			return fmt.Errorf("decode generation config: %w", err)
		}
	}
	return nil
}

func emptyGenerationConfig(v domain.GenerationConfig) bool {
	return v.Temperature == nil && v.TopP == nil && v.MaxOutputTokens == nil &&
		len(v.Stop) == 0 && v.Seed == nil
}

// ---- 行 → domain ----

func promptFromRow(r pgstore.Prompt) *domain.Prompt {
	return &domain.Prompt{
		ID:          r.ID.String(),
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Category:    r.Category,
		CreatedBy:   r.CreatedBy,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func versionFromRow(r pgstore.PromptVersion) *domain.PromptVersion {
	return &domain.PromptVersion{
		ID:              r.ID.String(),
		PromptID:        r.PromptID.String(),
		Version:         int(r.Version),
		Template:        r.Template,
		VariablesSchema: r.VariablesSchema,
		Hash:            r.Hash,
		TokenEstimate:   int(r.TokenEstimate),
		CreatedBy:       r.CreatedBy,
		CreatedAt:       r.CreatedAt,
	}
}

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return fmt.Errorf("get %s: %w", what, err)
}
