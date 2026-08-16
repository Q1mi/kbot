// Package audit 记录安全敏感操作的防篡改审计链。
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	ID           string         `json:"id"`
	WorkspaceID  string         `json:"workspace_id"`
	ActorID      string         `json:"actor_id"`
	Action       string         `json:"action"`
	ResourceID   string         `json:"resource_id"`
	Data         map[string]any `json:"data"`
	PreviousHash string         `json:"previous_hash"`
	Hash         string         `json:"hash"`
	CreatedAt    time.Time      `json:"created_at"`
}

func (l *Ledger) List(ctx context.Context, workspaceID string) ([]Event, error) {
	if l.pool != nil {
		return l.listPostgres(ctx, workspaceID)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	events := append([]Event(nil), l.events[workspaceID]...)
	for index := range events {
		events[index].Data = cloneMap(events[index].Data)
	}
	return events, nil
}

type Ledger struct {
	mu       sync.RWMutex
	events   map[string][]Event
	sequence atomic.Uint64
	now      func() time.Time
	pool     *pgxpool.Pool
}

func NewLedger() *Ledger { return &Ledger{events: make(map[string][]Event), now: time.Now} }

func NewPostgresLedger(pool *pgxpool.Pool) *Ledger {
	ledger := NewLedger()
	ledger.pool = pool
	return ledger
}

func (l *Ledger) Append(ctx context.Context, event Event) (Event, error) {
	if event.WorkspaceID == "" || event.ActorID == "" || event.Action == "" || event.ResourceID == "" {
		return Event{}, fmt.Errorf("workspace, actor, action and resource are required")
	}
	if l.pool != nil {
		return l.appendPostgres(ctx, event)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	chain := l.events[event.WorkspaceID]
	if len(chain) > 0 {
		event.PreviousHash = chain[len(chain)-1].Hash
	}
	event.ID = fmt.Sprintf("audit-%d", l.sequence.Add(1))
	event.CreatedAt = l.now().UTC()
	event.Data = cloneMap(event.Data)
	hash, err := eventHash(event)
	if err != nil {
		return Event{}, err
	}
	event.Hash = hash
	l.events[event.WorkspaceID] = append(chain, event)
	return event, nil
}

func (l *Ledger) Verify(ctx context.Context, workspaceID string) error {
	if l.pool != nil {
		events, err := l.listPostgres(ctx, workspaceID)
		if err != nil {
			return err
		}
		return verifyEvents(events)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return verifyEvents(l.events[workspaceID])
}

func verifyEvents(events []Event) error {
	previous := ""
	for index, event := range events {
		if event.PreviousHash != previous {
			return fmt.Errorf("audit chain broken at index %d", index)
		}
		hash, err := eventHash(event)
		if err != nil || hash != event.Hash {
			return fmt.Errorf("audit event %s hash mismatch", event.ID)
		}
		previous = event.Hash
	}
	return nil
}

func (l *Ledger) appendPostgres(ctx context.Context, event Event) (Event, error) {
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Event{}, fmt.Errorf("begin audit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, event.WorkspaceID); err != nil {
		return Event{}, fmt.Errorf("lock audit chain: %w", err)
	}
	if err = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&event.ID); err != nil {
		return Event{}, fmt.Errorf("allocate audit id: %w", err)
	}
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_events WHERE workspace_id=$1 ORDER BY created_at DESC, id DESC LIMIT 1`, event.WorkspaceID).Scan(&event.PreviousHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Event{}, fmt.Errorf("load audit chain head: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		event.PreviousHash = ""
	}
	event.CreatedAt = l.now().UTC()
	event.Data = cloneMap(event.Data)
	event.Hash, err = eventHash(event)
	if err != nil {
		return Event{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events (id, workspace_id, actor_id, action, resource_id, data, previous_hash, hash, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		event.ID, event.WorkspaceID, event.ActorID, event.Action, event.ResourceID, event.Data, event.PreviousHash, event.Hash, event.CreatedAt); err != nil {
		return Event{}, fmt.Errorf("insert audit event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Event{}, fmt.Errorf("commit audit event: %w", err)
	}
	return event, nil
}

func (l *Ledger) listPostgres(ctx context.Context, workspaceID string) ([]Event, error) {
	rows, err := l.pool.Query(ctx, `SELECT id, workspace_id, actor_id, action, resource_id, data, previous_hash, hash, created_at FROM audit_events WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.WorkspaceID, &event.ActorID, &event.Action, &event.ResourceID, &event.Data, &event.PreviousHash, &event.Hash, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return events, nil
}

func eventHash(event Event) (string, error) {
	payload := struct {
		ID, WorkspaceID, ActorID, Action, ResourceID string
		Data                                         map[string]any
		PreviousHash                                 string
		CreatedAt                                    time.Time
	}{
		event.ID, event.WorkspaceID, event.ActorID, event.Action, event.ResourceID, event.Data, event.PreviousHash, event.CreatedAt,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal audit event: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum), nil
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
