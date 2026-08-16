package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
)

func TestCreateProfileReturnsConflictForDuplicateName(t *testing.T) {
	ctx := context.Background()
	cipher, err := modelconfig.NewCipher([]byte("test-encryption-key"))
	if err != nil {
		t.Fatal(err)
	}
	svc := modelconfig.NewService(modelconfig.NewMemoryStore(), cipher)
	account, err := svc.CreateProviderAccount(ctx, modelconfig.CreateProviderAccountRequest{
		WorkspaceID: "w1", Name: "provider", Kind: "openai-compatible",
		BaseURL: "https://example.com/v1", APIKey: "sk-test", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := svc.CreateDeployment(ctx, modelconfig.CreateDeploymentRequest{
		WorkspaceID: "w1", ProviderAccountID: account.ID, Name: "primary",
		ModelName: "model-a", CreatedBy: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateProfile(ctx, modelconfig.CreateProfileRequest{
		WorkspaceID: "w1", Name: "商品运营 Profile", PrimaryDeploymentID: deployment.ID,
		CreatedBy: "u1",
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"商品运营 Profile","primary_deployment_id":"` + deployment.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/model-profiles", strings.NewReader(body))
	reqCtx := context.WithValue(req.Context(), middleware.ContextKeyWorkspaceID, "w1")
	reqCtx = context.WithValue(reqCtx, middleware.ContextKeyUserID, "u1")
	recorder := httptest.NewRecorder()

	NewModelConfigHandler(svc).CreateProfile(recorder, req.WithContext(reqCtx))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "同一工作空间内已存在同名 Profile") {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content type=%q", contentType)
	}
}
