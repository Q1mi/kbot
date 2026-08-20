package llm

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type staticProfileResolver struct {
	profile *ResolvedModelProfile
}

func (r staticProfileResolver) ResolveProfile(context.Context, string) (*ResolvedModelProfile, error) {
	return r.profile, nil
}

func TestPrepareExecutionBuildsADKRetryAndFailover(t *testing.T) {
	g := &Gateway{sink: NopSink{}}
	g.WithProfileResolver(staticProfileResolver{profile: &ResolvedModelProfile{
		VersionID: "profile-v1", ClassificationMax: "secret",
		Deployments: []ResolvedDeployment{
			{ID: "primary", ProviderID: "p1", BaseURL: "https://primary.example/v1", APIKey: "test", Model: "model-a", MaxRetries: 1},
			{ID: "backup", ProviderID: "p2", BaseURL: "https://backup.example/v1", APIKey: "test", Model: "model-b", MaxRetries: 3},
		},
	}})
	ctx := WithInvocationConfig(context.Background(), InvocationConfig{ModelProfileVersionID: "profile-v1"})

	plan, err := g.PrepareExecution(ctx)
	if err != nil {
		t.Fatalf("prepare execution: %v", err)
	}
	if plan.Model == nil {
		t.Fatal("primary model is nil")
	}
	if plan.Retry == nil || plan.Retry.MaxRetries != 3 {
		t.Fatalf("retry config = %+v", plan.Retry)
	}
	if plan.Failover == nil || plan.Failover.MaxRetries != 1 {
		t.Fatalf("failover config = %+v", plan.Failover)
	}
	failoverModel, replacement, err := plan.Failover.GetFailoverModel(context.Background(), &adk.FailoverContext[*schema.Message]{
		FailoverAttempt: 1,
	})
	if err != nil || failoverModel == nil || replacement != nil {
		t.Fatalf("resolve failover model: model=%v replacement=%v err=%v", failoverModel, replacement, err)
	}
}
