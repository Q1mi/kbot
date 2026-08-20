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
