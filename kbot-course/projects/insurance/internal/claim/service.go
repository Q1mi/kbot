// Package claim 是保险理赔 Agent 的垂直领域核心。
package claim

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("claim triage workflow is implemented in 22-end")

type Claim struct {
	ID, PolicyID, Reason string
	Amount               float64
	Documents            []string
}
type Decision struct {
	Eligible, RequiresApproval bool
	RiskScore                  float64
	Action                     string
	Evidence                   []string
}
type Policy struct {
	ID                              string
	Active                          bool
	CoverageLimit, AutoApproveLimit float64
}
type PolicyReader interface {
	GetPolicy(context.Context, string) (Policy, error)
}
type FraudScorer interface {
	Score(context.Context, Claim) (float64, error)
}
type Service struct{}

func NewService(PolicyReader, FraudScorer) *Service { return &Service{} }
func (s *Service) Triage(context.Context, Claim) (Decision, error) {
	return Decision{}, ErrNotImplemented
}
