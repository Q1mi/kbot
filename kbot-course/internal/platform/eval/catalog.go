package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Dataset struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	TargetKind  string    `json:"target_kind"`
	CreatedAt   time.Time `json:"created_at"`
}

type StoredCase struct {
	ID        string    `json:"id"`
	DatasetID string    `json:"dataset_id"`
	Input     string    `json:"input"`
	Expected  string    `json:"expected"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

type StoredRun struct {
	ID             string    `json:"id"`
	DatasetID      string    `json:"dataset_id"`
	AgentID        string    `json:"agent_id"`
	AgentVersionID string    `json:"agent_version_id"`
	JudgeKind      string    `json:"judge_kind"`
	Threshold      float64   `json:"threshold"`
	PassRate       float64   `json:"pass_rate"`
	Passed         bool      `json:"passed"`
	Report         Report    `json:"report"`
	CreatedAt      time.Time `json:"created_at"`
}

type Catalog struct {
	mu       sync.RWMutex
	datasets map[string]Dataset
	cases    map[string][]StoredCase
	runs     map[string][]StoredRun
	sequence atomic.Uint64
	pool     *pgxpool.Pool
}

func NewCatalog() *Catalog {
	return &Catalog{datasets: make(map[string]Dataset), cases: make(map[string][]StoredCase), runs: make(map[string][]StoredRun)}
}

func NewPostgresCatalog(pool *pgxpool.Pool) *Catalog {
	catalog := NewCatalog()
	catalog.pool = pool
	return catalog
}

func (c *Catalog) CreateDataset(ctx context.Context, workspaceID, name, targetKind string) (Dataset, error) {
	if workspaceID == "" || name == "" {
		return Dataset{}, fmt.Errorf("workspace and dataset name are required")
	}
	if targetKind == "" {
		targetKind = "agent"
	}
	if targetKind != "agent" {
		return Dataset{}, fmt.Errorf("unsupported target kind %q", targetKind)
	}
	if c.pool != nil {
		var dataset Dataset
		err := c.pool.QueryRow(ctx, `INSERT INTO eval_datasets (id,workspace_id,name,target_kind) VALUES (gen_random_uuid()::text,$1,$2,$3) RETURNING id,workspace_id,name,target_kind,created_at`, workspaceID, name, targetKind).
			Scan(&dataset.ID, &dataset.WorkspaceID, &dataset.Name, &dataset.TargetKind, &dataset.CreatedAt)
		return dataset, err
	}
	dataset := Dataset{ID: c.nextID("dataset"), WorkspaceID: workspaceID, Name: name, TargetKind: targetKind, CreatedAt: time.Now().UTC()}
	c.mu.Lock()
	c.datasets[dataset.ID] = dataset
	c.mu.Unlock()
	return dataset, nil
}

func (c *Catalog) ListDatasets(ctx context.Context, workspaceID string) ([]Dataset, error) {
	if c.pool != nil {
		rows, err := c.pool.Query(ctx, `SELECT id,workspace_id,name,target_kind,created_at FROM eval_datasets WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]Dataset, 0)
		for rows.Next() {
			var dataset Dataset
			if err := rows.Scan(&dataset.ID, &dataset.WorkspaceID, &dataset.Name, &dataset.TargetKind, &dataset.CreatedAt); err != nil {
				return nil, err
			}
			result = append(result, dataset)
		}
		return result, rows.Err()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]Dataset, 0)
	for _, dataset := range c.datasets {
		if dataset.WorkspaceID == workspaceID {
			result = append(result, dataset)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (c *Catalog) AddCase(ctx context.Context, workspaceID, datasetID, input, expected, metadata string) (StoredCase, error) {
	if input == "" {
		return StoredCase{}, fmt.Errorf("case input is required")
	}
	if c.pool != nil {
		var testCase StoredCase
		err := c.pool.QueryRow(ctx, `INSERT INTO eval_cases (id,dataset_id,input,expected,metadata)
			SELECT gen_random_uuid()::text,d.id,$3,$4,$5 FROM eval_datasets d WHERE d.id=$2 AND d.workspace_id=$1
			RETURNING id,dataset_id,input,expected,metadata,created_at`, workspaceID, datasetID, input, expected, metadata).
			Scan(&testCase.ID, &testCase.DatasetID, &testCase.Input, &testCase.Expected, &testCase.Metadata, &testCase.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredCase{}, fmt.Errorf("dataset %s not found", datasetID)
		}
		return testCase, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dataset, ok := c.datasets[datasetID]
	if !ok || dataset.WorkspaceID != workspaceID {
		return StoredCase{}, fmt.Errorf("dataset %s not found", datasetID)
	}
	testCase := StoredCase{ID: c.nextID("case"), DatasetID: datasetID, Input: input, Expected: expected, Metadata: metadata, CreatedAt: time.Now().UTC()}
	c.cases[datasetID] = append(c.cases[datasetID], testCase)
	return testCase, nil
}

func (c *Catalog) Cases(ctx context.Context, workspaceID, datasetID string) ([]StoredCase, error) {
	if c.pool != nil {
		rows, err := c.pool.Query(ctx, `SELECT c.id,c.dataset_id,c.input,c.expected,c.metadata,c.created_at FROM eval_cases c JOIN eval_datasets d ON d.id=c.dataset_id WHERE d.workspace_id=$1 AND d.id=$2 ORDER BY c.created_at,c.id`, workspaceID, datasetID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]StoredCase, 0)
		for rows.Next() {
			var testCase StoredCase
			if err := rows.Scan(&testCase.ID, &testCase.DatasetID, &testCase.Input, &testCase.Expected, &testCase.Metadata, &testCase.CreatedAt); err != nil {
				return nil, err
			}
			result = append(result, testCase)
		}
		if len(result) == 0 {
			var exists bool
			if err := c.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM eval_datasets WHERE workspace_id=$1 AND id=$2)`, workspaceID, datasetID).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("dataset %s not found", datasetID)
			}
		}
		return result, rows.Err()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	dataset, ok := c.datasets[datasetID]
	if !ok || dataset.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("dataset %s not found", datasetID)
	}
	return append([]StoredCase(nil), c.cases[datasetID]...), nil
}

func (c *Catalog) RecordRun(ctx context.Context, workspaceID string, run StoredRun) (StoredRun, error) {
	if run.DatasetID == "" || run.AgentID == "" || run.AgentVersionID == "" || run.JudgeKind == "" {
		return StoredRun{}, fmt.Errorf("dataset, target version and judge are required")
	}
	if c.pool != nil {
		report, err := json.Marshal(run.Report)
		if err != nil {
			return StoredRun{}, err
		}
		err = c.pool.QueryRow(ctx, `INSERT INTO eval_runs (id,dataset_id,agent_id,agent_version_id,judge_kind,threshold,pass_rate,passed,report)
			SELECT gen_random_uuid()::text,d.id,$3,$4,$5,$6,$7,$8,$9 FROM eval_datasets d WHERE d.workspace_id=$1 AND d.id=$2
			RETURNING id,created_at`, workspaceID, run.DatasetID, run.AgentID, run.AgentVersionID, run.JudgeKind, run.Threshold, run.PassRate, run.Passed, report).
			Scan(&run.ID, &run.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredRun{}, fmt.Errorf("dataset %s not found", run.DatasetID)
		}
		return run, err
	}
	run.ID, run.CreatedAt = c.nextID("eval-run"), time.Now().UTC()
	c.mu.Lock()
	c.runs[run.DatasetID] = append(c.runs[run.DatasetID], run)
	c.mu.Unlock()
	return run, nil
}

func (c *Catalog) ListRuns(ctx context.Context, workspaceID, datasetID string) ([]StoredRun, error) {
	if c.pool != nil {
		rows, err := c.pool.Query(ctx, `SELECT r.id,r.dataset_id,r.agent_id,r.agent_version_id,r.judge_kind,r.threshold,r.pass_rate,r.passed,r.report,r.created_at FROM eval_runs r JOIN eval_datasets d ON d.id=r.dataset_id WHERE d.workspace_id=$1 AND d.id=$2 ORDER BY r.created_at DESC,r.id DESC`, workspaceID, datasetID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]StoredRun, 0)
		for rows.Next() {
			var run StoredRun
			var report []byte
			if err := rows.Scan(&run.ID, &run.DatasetID, &run.AgentID, &run.AgentVersionID, &run.JudgeKind, &run.Threshold, &run.PassRate, &run.Passed, &report, &run.CreatedAt); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(report, &run.Report); err != nil {
				return nil, err
			}
			result = append(result, run)
		}
		return result, rows.Err()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if dataset, ok := c.datasets[datasetID]; !ok || dataset.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("dataset %s not found", datasetID)
	}
	result := append([]StoredRun(nil), c.runs[datasetID]...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (c *Catalog) RequirePassedRun(ctx context.Context, workspaceID, agentID, versionID string) error {
	if c.pool != nil {
		var exists bool
		if err := c.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM eval_runs r JOIN eval_datasets d ON d.id=r.dataset_id WHERE d.workspace_id=$1 AND r.agent_id=$2 AND r.agent_version_id=$3 AND r.passed)`, workspaceID, agentID, versionID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("agent version %s has no passed evaluation run", versionID)
		}
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for datasetID, runs := range c.runs {
		dataset := c.datasets[datasetID]
		if dataset.WorkspaceID != workspaceID {
			continue
		}
		for _, run := range runs {
			if run.AgentID == agentID && run.AgentVersionID == versionID && run.Passed {
				return nil
			}
		}
	}
	return fmt.Errorf("agent version %s has no passed evaluation run", versionID)
}

func (c *Catalog) nextID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, c.sequence.Add(1))
}
