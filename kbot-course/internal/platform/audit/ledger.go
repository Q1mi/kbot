// Package audit 记录安全敏感操作的防篡改审计链。
package audit

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("audit ledger is implemented in 19-end")

type Event struct {
	ID, WorkspaceID, ActorID, Action, ResourceID string
	Data                                         map[string]any
	Hash                                         string
}
type Ledger struct{}

func NewLedger() *Ledger                                       { return &Ledger{} }
func (l *Ledger) Append(context.Context, Event) (Event, error) { return Event{}, ErrNotImplemented }
func (l *Ledger) Verify(context.Context, string) error         { return ErrNotImplemented }
