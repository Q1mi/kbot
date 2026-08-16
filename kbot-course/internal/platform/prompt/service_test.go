package prompt

import (
	"context"
	"testing"
)

func TestPublishedPromptRendersPinnedTemplate(t *testing.T) {
	service := NewService()
	version := Version{ID: "support-v1", WorkspaceID: "ws", Name: "support", Template: "你好，{{.customer}}"}
	if err := service.Publish(context.Background(), version); err != nil {
		t.Fatal(err)
	}
	output, err := service.Render(context.Background(), "ws", "support-v1", map[string]string{"customer": "小明"})
	if err != nil || output != "你好，小明" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if err := service.Publish(context.Background(), version); err == nil {
		t.Fatal("expected immutable version conflict")
	}
	if _, err := service.Render(context.Background(), "ws", "support-v1", nil); err == nil {
		t.Fatal("expected missing variable error")
	}
}

func TestPromptVersionsPromoteAndRenderByEnvironment(t *testing.T) {
	service := NewService()
	item, first, err := service.Create(t.Context(), "ws", "support", "chat", "v1 {{.name}}", `{}`, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateVersion(t.Context(), "ws", item.ID, "v2 {{.name}}", `{}`, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Promote("ws", item.ID, "prod", second.ID); err != nil {
		t.Fatal(err)
	}
	output, err := service.RenderEnvironment("ws", item.ID, "prod", map[string]any{"name": "kbot"})
	if err != nil || output != "v2 kbot" || first.Hash == "" || second.Version != 2 {
		t.Fatalf("first=%+v second=%+v output=%q err=%v", first, second, output, err)
	}
}
