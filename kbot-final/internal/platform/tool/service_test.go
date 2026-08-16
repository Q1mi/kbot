package tool_test

import (
	"context"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/util"
)

func TestCreateToolRejectsInvalidConfigurationBeforePersisting(t *testing.T) {
	store := platform.NewMemoryToolStore()
	service := tool.NewService(store)
	_, err := service.CreateTool(context.Background(), tool.CreateToolRequest{
		WorkspaceID: "workspace-1",
		Name:        "broken",
		SourceType:  "rest_api",
		SchemaJSON:  `{`,
	})
	if err == nil {
		t.Fatal("invalid JSON configuration was accepted")
	}
	tools, listErr := service.ListTools(context.Background(), "workspace-1")
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(tools) != 0 {
		t.Fatalf("invalid tool was persisted: %+v", tools)
	}
}

func TestPublishGateUsesSuccessfulRunFromSameVersion(t *testing.T) {
	ctx := context.Background()
	store := platform.NewMemoryToolStore()
	service := tool.NewService(store)
	created, err := service.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "workspace-1", Name: "orders", SourceType: "internal_sdk",
		SchemaJSON: `{}`, EndpointConfig: `{"sdk_name":"orders"}`, AuthConfig: `{}`, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := store.GetToolCurrentVersion(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTestRun(ctx, &domain.ToolTestRun{
		ID: util.GenerateID(), ToolID: created.ID, ToolVersionID: v1.ID,
		Input: `{}`, Output: `ok`, Status: "success", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	v2, err := service.CreateToolVersion(ctx, created.ID, tool.CreateToolVersionRequest{
		EndpointConfig: `{"sdk_name":"orders_v2"}`, CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishToolVersion(ctx, created.ID, v2.ID); err == nil {
		t.Fatal("v2 inherited v1 test result")
	}
	if err := store.CreateTestRun(ctx, &domain.ToolTestRun{
		ID: util.GenerateID(), ToolID: created.ID, ToolVersionID: v2.ID,
		Input: `{}`, Output: `ok-v2`, Status: "success", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishToolVersion(ctx, created.ID, v2.ID); err != nil {
		t.Fatalf("publish tested v2: %v", err)
	}
	currentID, err := service.GetToolCurrentVersionID(ctx, created.ID)
	if err != nil || currentID != v2.ID {
		t.Fatalf("latest published version mismatch: id=%s err=%v", currentID, err)
	}
}
