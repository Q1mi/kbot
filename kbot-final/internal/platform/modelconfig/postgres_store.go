package modelconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/runtime/llm"
)

type PostgresStore struct{ db *pgxpool.Pool }

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore { return &PostgresStore{db: db} }

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) CreateProviderAccount(ctx context.Context, a *providerAccountRecord) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO providers
		  (id, workspace_id, name, kind, credential_ref, api_key_ciphertext, base_url, status, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,'',$5,$6,$7,$8,now(),now())`,
		id, a.WorkspaceID, a.Name, a.Kind, a.APIKeyCiphertext, a.BaseURL, a.Status, a.CreatedBy)
	return wrap("create provider account", err)
}

func (s *PostgresStore) UpdateProviderAPIKey(ctx context.Context, id string, ciphertext []byte) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE providers SET api_key_ciphertext=$2,updated_at=now() WHERE id=$1`,
		uid, ciphertext)
	if err != nil {
		return wrap("rotate provider api key", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("provider account not found")
	}
	return nil
}

func (s *PostgresStore) ListProviderAccounts(ctx context.Context, workspaceID string) ([]*providerAccountRecord, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, workspace_id, name, kind, base_url, status, api_key_ciphertext,
		       created_by, created_at, updated_at
		FROM providers WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*providerAccountRecord
	for rows.Next() {
		v := &providerAccountRecord{}
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.Kind, &v.BaseURL, &v.Status,
			&v.APIKeyCiphertext, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.HasAPIKey = len(v.APIKeyCiphertext) > 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetProviderAccount(ctx context.Context, id string) (*providerAccountRecord, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	v := &providerAccountRecord{}
	err = s.db.QueryRow(ctx, `
		SELECT id::text, workspace_id, name, kind, base_url, status, api_key_ciphertext,
		       created_by, created_at, updated_at
		FROM providers WHERE id=$1`, uid).
		Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.Kind, &v.BaseURL, &v.Status,
			&v.APIKeyCiphertext, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, notFound("provider account", err)
	}
	v.HasAPIKey = len(v.APIKeyCiphertext) > 0
	return v, nil
}

func (s *PostgresStore) CreateDeployment(ctx context.Context, d *ModelDeployment) error {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(d.ProviderAccountID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO model_deployments
		  (id,workspace_id,provider_account_id,name,model_name,region,timeout_ms,max_retries,
		   input_price_per_million,output_price_per_million,cached_input_price_per_million,status,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		id, d.WorkspaceID, accountID, d.Name, d.ModelName, d.Region, d.TimeoutMS, d.MaxRetries,
		d.InputPricePerMillion, d.OutputPricePerMillion, d.CachedInputPricePerMillion, d.Status, d.CreatedBy)
	return wrap("create model deployment", err)
}

func (s *PostgresStore) UpdateDeploymentPricing(
	ctx context.Context, id string, inputPrice, outputPrice, cachedInputPrice float64,
) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE model_deployments
		SET input_price_per_million=$2,output_price_per_million=$3,
		    cached_input_price_per_million=$4,updated_at=now()
		WHERE id=$1`, uid, inputPrice, outputPrice, cachedInputPrice)
	if err != nil {
		return wrap("update model deployment pricing", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("model deployment not found")
	}
	return nil
}

func (s *PostgresStore) ListDeployments(ctx context.Context, workspaceID string) ([]*ModelDeployment, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text,workspace_id,provider_account_id::text,name,model_name,region,
		       timeout_ms,max_retries,input_price_per_million,output_price_per_million,
		       cached_input_price_per_million,status,created_by,created_at,updated_at
		FROM model_deployments WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ModelDeployment
	for rows.Next() {
		v := &ModelDeployment{}
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.ProviderAccountID, &v.Name, &v.ModelName,
			&v.Region, &v.TimeoutMS, &v.MaxRetries, &v.InputPricePerMillion, &v.OutputPricePerMillion,
			&v.CachedInputPricePerMillion, &v.Status, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetDeployment(ctx context.Context, id string) (*ModelDeployment, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	v := &ModelDeployment{}
	err = s.db.QueryRow(ctx, `
		SELECT id::text,workspace_id,provider_account_id::text,name,model_name,region,
		       timeout_ms,max_retries,input_price_per_million,output_price_per_million,
		       cached_input_price_per_million,status,created_by,created_at,updated_at
		FROM model_deployments WHERE id=$1`, uid).
		Scan(&v.ID, &v.WorkspaceID, &v.ProviderAccountID, &v.Name, &v.ModelName,
			&v.Region, &v.TimeoutMS, &v.MaxRetries, &v.InputPricePerMillion, &v.OutputPricePerMillion,
			&v.CachedInputPricePerMillion, &v.Status, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, notFound("model deployment", err)
	}
	return v, nil
}

func (s *PostgresStore) CreateProfile(ctx context.Context, p *ModelProfile) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO model_profiles (id,workspace_id,name,description,created_by)
		VALUES ($1,$2,$3,$4,$5)`, id, p.WorkspaceID, p.Name, p.Description, p.CreatedBy)
	if isProfileNameConflict(err) {
		return ErrProfileNameExists
	}
	return wrap("create model profile", err)
}

func (s *PostgresStore) CreateProfileWithVersion(ctx context.Context, p *ModelProfile, v *ModelProfileVersion) error {
	profileID, err := uuid.Parse(p.ID)
	if err != nil {
		return err
	}
	versionID, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	primaryID, err := uuid.Parse(v.PrimaryDeploymentID)
	if err != nil {
		return err
	}
	fallbackJSON, err := json.Marshal(v.FallbackDeploymentIDs)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO model_profiles (id,workspace_id,name,description,created_by)
		VALUES ($1,$2,$3,$4,$5)`, profileID, p.WorkspaceID, p.Name, p.Description, p.CreatedBy); err != nil {
		return wrap("create model profile", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO model_profile_versions
		  (id,profile_id,version,primary_deployment_id,fallback_deployment_ids,classification_max,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, versionID, profileID, v.Version, primaryID,
		fallbackJSON, v.ClassificationMax, v.CreatedBy); err != nil {
		return wrap("create model profile version", err)
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) ListProfiles(ctx context.Context, workspaceID string) ([]*ModelProfile, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text,workspace_id,name,description,created_by,created_at,updated_at
		FROM model_profiles WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ModelProfile
	for rows.Next() {
		v := &ModelProfile{}
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.Description, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetProfile(ctx context.Context, id string) (*ModelProfile, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	v := &ModelProfile{}
	err = s.db.QueryRow(ctx, `
		SELECT id::text,workspace_id,name,description,created_by,created_at,updated_at
		FROM model_profiles WHERE id=$1`, uid).
		Scan(&v.ID, &v.WorkspaceID, &v.Name, &v.Description, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, notFound("model profile", err)
	}
	return v, nil
}

func (s *PostgresStore) CreateProfileVersion(ctx context.Context, v *ModelProfileVersion) error {
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return err
	}
	profileID, err := uuid.Parse(v.ProfileID)
	if err != nil {
		return err
	}
	primaryID, err := uuid.Parse(v.PrimaryDeploymentID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO model_profile_versions
		  (id,profile_id,version,primary_deployment_id,fallback_deployment_ids,classification_max,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		id, profileID, v.Version, primaryID, encodeStrings(v.FallbackDeploymentIDs), v.ClassificationMax, v.CreatedBy)
	return wrap("create model profile version", err)
}

func (s *PostgresStore) ListProfileVersions(ctx context.Context, profileID string) ([]*ModelProfileVersion, error) {
	uid, err := uuid.Parse(profileID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id::text,profile_id::text,version,primary_deployment_id::text,
		       fallback_deployment_ids,classification_max,created_by,created_at
		FROM model_profile_versions WHERE profile_id=$1 ORDER BY version`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ModelProfileVersion
	for rows.Next() {
		v, err := scanProfileVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetProfileVersion(ctx context.Context, id string) (*ModelProfileVersion, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(ctx, `
		SELECT id::text,profile_id::text,version,primary_deployment_id::text,
		       fallback_deployment_ids,classification_max,created_by,created_at
		FROM model_profile_versions WHERE id=$1`, uid)
	v, err := scanProfileVersion(row)
	if err != nil {
		return nil, notFound("model profile version", err)
	}
	return v, nil
}

func (s *PostgresStore) UpsertProjectBinding(ctx context.Context, b *ProjectBinding) error {
	vid, err := uuid.Parse(b.ModelProfileVersionID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO project_model_bindings
		  (workspace_id,env,model_profile_version_id,monthly_budget,rpm_limit,tpm_limit,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (workspace_id,env) DO UPDATE SET
		  model_profile_version_id=EXCLUDED.model_profile_version_id,
		  monthly_budget=EXCLUDED.monthly_budget,rpm_limit=EXCLUDED.rpm_limit,
		  tpm_limit=EXCLUDED.tpm_limit,updated_at=now()`,
		b.WorkspaceID, b.Env, vid, b.MonthlyBudget, b.RPMLimit, b.TPMLimit)
	return wrap("upsert project model binding", err)
}

func (s *PostgresStore) GetProjectBinding(ctx context.Context, workspaceID, env string) (*ProjectBinding, error) {
	b := &ProjectBinding{WorkspaceID: workspaceID, Env: env}
	err := s.db.QueryRow(ctx, `
		SELECT model_profile_version_id::text,monthly_budget,rpm_limit,tpm_limit
		FROM project_model_bindings WHERE workspace_id=$1 AND env=$2`, workspaceID, env).
		Scan(&b.ModelProfileVersionID, &b.MonthlyBudget, &b.RPMLimit, &b.TPMLimit)
	if err != nil {
		return nil, notFound("project model binding", err)
	}
	return b, nil
}

func (s *PostgresStore) ReserveProjectUsage(ctx context.Context, req llm.ProjectQuotaRequest) (string, error) {
	profileID, err := uuid.Parse(req.ModelProfileVersionID)
	if err != nil {
		return "", fmt.Errorf("parse model profile version id: %w", err)
	}
	deploymentID, err := uuid.Parse(req.DeploymentID)
	if err != nil {
		return "", fmt.Errorf("parse deployment id: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var binding ProjectBinding
	binding.WorkspaceID, binding.Env = req.WorkspaceID, req.Env
	err = tx.QueryRow(ctx, `
		SELECT model_profile_version_id::text,monthly_budget,rpm_limit,tpm_limit
		FROM project_model_bindings
		WHERE workspace_id=$1 AND env=$2 FOR UPDATE`, req.WorkspaceID, req.Env).
		Scan(&binding.ModelProfileVersionID, &binding.MonthlyBudget, &binding.RPMLimit, &binding.TPMLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load project model binding: %w", err)
	}
	if binding.ModelProfileVersionID != req.ModelProfileVersionID {
		return "", fmt.Errorf("%w: profile is not bound to workspace environment %s", llm.ErrProjectQuotaExceeded, req.Env)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_model_usage_reservations
		SET status='failed',actual_tokens=0,actual_cost=0,finalized_at=now()
		WHERE workspace_id=$1 AND env=$2 AND status='reserved' AND expires_at <= now()`,
		req.WorkspaceID, req.Env); err != nil {
		return "", fmt.Errorf("reconcile expired project model usage: %w", err)
	}

	now := time.Now().UTC()
	minute := now.Truncate(time.Minute)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var calls int
	var minuteTokens int64
	if err := tx.QueryRow(ctx, `
		SELECT count(*),COALESCE(sum(CASE WHEN status='reserved' THEN reserved_tokens ELSE actual_tokens END),0)
		FROM project_model_usage_reservations
		WHERE workspace_id=$1 AND env=$2 AND minute=$3`, req.WorkspaceID, req.Env, minute).
		Scan(&calls, &minuteTokens); err != nil {
		return "", fmt.Errorf("read minute model usage: %w", err)
	}
	var monthlyCost float64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(sum(CASE WHEN status='reserved' THEN reserved_cost ELSE actual_cost END),0)
		FROM project_model_usage_reservations
		WHERE workspace_id=$1 AND env=$2 AND month=$3`, req.WorkspaceID, req.Env, month).
		Scan(&monthlyCost); err != nil {
		return "", fmt.Errorf("read monthly model usage: %w", err)
	}
	if binding.RPMLimit > 0 && calls+1 > binding.RPMLimit {
		return "", fmt.Errorf("%w: RPM limit %d reached", llm.ErrProjectQuotaExceeded, binding.RPMLimit)
	}
	if binding.TPMLimit > 0 && minuteTokens+int64(req.ReservedTokens) > int64(binding.TPMLimit) {
		return "", fmt.Errorf("%w: TPM limit %d would be exceeded", llm.ErrProjectQuotaExceeded, binding.TPMLimit)
	}
	if binding.MonthlyBudget > 0 && monthlyCost+req.ReservedCost > binding.MonthlyBudget {
		return "", fmt.Errorf("%w: monthly budget %.4f would be exceeded", llm.ErrProjectQuotaExceeded, binding.MonthlyBudget)
	}

	reservationID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_model_usage_reservations
		  (id,workspace_id,env,model_profile_version_id,deployment_id,minute,month,reserved_tokens,reserved_cost,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now() + interval '1 hour')`, reservationID, req.WorkspaceID, req.Env,
		profileID, deploymentID, minute, month, req.ReservedTokens, req.ReservedCost); err != nil {
		return "", fmt.Errorf("reserve project model usage: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return reservationID.String(), nil
}

func (s *PostgresStore) FinalizeProjectUsage(
	ctx context.Context, reservationID string, actualTokens int, actualCost float64, success bool,
) error {
	if reservationID == "" {
		return nil
	}
	id, err := uuid.Parse(reservationID)
	if err != nil {
		return err
	}
	status := "failed"
	if success {
		status = "completed"
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE project_model_usage_reservations
		SET actual_tokens=$2,actual_cost=$3,status=$4,finalized_at=now()
		WHERE id=$1 AND status='reserved'`, id, actualTokens, actualCost, status)
	if err != nil {
		return fmt.Errorf("finalize project model usage: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("project model usage reservation is unavailable")
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanProfileVersion(row rowScanner) (*ModelProfileVersion, error) {
	v := &ModelProfileVersion{}
	var fallbackJSON []byte
	if err := row.Scan(&v.ID, &v.ProfileID, &v.Version, &v.PrimaryDeploymentID,
		&fallbackJSON, &v.ClassificationMax, &v.CreatedBy, &v.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(fallbackJSON, &v.FallbackDeploymentIDs); err != nil {
		return nil, fmt.Errorf("decode fallback deployments: %w", err)
	}
	return v, nil
}

func notFound(what string, err error) error {
	if err == pgx.ErrNoRows {
		return fmt.Errorf("%s not found", what)
	}
	return err
}

func wrap(op string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func isProfileNameConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "model_profiles_workspace_id_name_key"
}
