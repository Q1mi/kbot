package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/tool"
)

// --- 测试用的最小 tool.Store ---

type memToolStore struct {
	tools    map[string]*domain.Tool
	versions map[string]*domain.ToolVersion
	current  map[string]string
	testRuns map[string][]*domain.ToolTestRun
}

func newMemToolStore() *memToolStore {
	return &memToolStore{
		tools:    map[string]*domain.Tool{},
		versions: map[string]*domain.ToolVersion{},
		current:  map[string]string{},
		testRuns: map[string][]*domain.ToolTestRun{},
	}
}

func (s *memToolStore) GetTool(_ context.Context, id string) (*domain.Tool, error) {
	if t, ok := s.tools[id]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("not found")
}
func (s *memToolStore) CreateTool(_ context.Context, t *domain.Tool) error {
	s.tools[t.ID] = t
	return nil
}
func (s *memToolStore) ListTools(_ context.Context, ws string) ([]*domain.Tool, error) {
	return nil, nil
}
func (s *memToolStore) GetToolVersion(_ context.Context, id string) (*domain.ToolVersion, error) {
	return s.versions[id], nil
}
func (s *memToolStore) CreateToolVersion(_ context.Context, v *domain.ToolVersion) error {
	s.versions[v.ID] = v
	s.current[v.ToolID] = v.ID
	return nil
}
func (s *memToolStore) GetToolCurrentVersion(_ context.Context, toolID string) (*domain.ToolVersion, error) {
	id, ok := s.current[toolID]
	if !ok {
		return nil, fmt.Errorf("no version")
	}
	return s.versions[id], nil
}
func (s *memToolStore) GetToolLatestPublishedVersion(ctx context.Context, toolID string) (*domain.ToolVersion, error) {
	return s.GetToolCurrentVersion(ctx, toolID)
}
func (s *memToolStore) ListToolVersions(ctx context.Context, toolID string) ([]*domain.ToolVersion, error) {
	v, err := s.GetToolCurrentVersion(ctx, toolID)
	if err != nil {
		return nil, err
	}
	return []*domain.ToolVersion{v}, nil
}
func (s *memToolStore) UpdateToolVersionStatus(_ context.Context, id, status string) error {
	if v, ok := s.versions[id]; ok {
		v.Status = status
	}
	return nil
}
func (s *memToolStore) ListLegacyToolAuthVersions(_ context.Context) ([]*domain.ToolVersion, error) {
	return nil, nil
}
func (s *memToolStore) EncryptToolVersionAuth(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (s *memToolStore) CreateInvocation(_ context.Context, _ *domain.ToolInvocation) error {
	return nil
}
func (s *memToolStore) CompleteInvocation(_ context.Context, _, _, _ string, _ int, _ string) error {
	return nil
}
func (s *memToolStore) CreateSandboxExecution(_ context.Context, _ *domain.SandboxExecution) error {
	return nil
}
func (s *memToolStore) CreateTestRun(_ context.Context, r *domain.ToolTestRun) error {
	s.testRuns[r.ToolID] = append(s.testRuns[r.ToolID], r)
	return nil
}
func (s *memToolStore) GetToolLastSuccessfulTestRun(_ context.Context, toolID string) (*domain.ToolTestRun, error) {
	runs := s.testRuns[toolID]
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == "success" {
			return runs[i], nil
		}
	}
	return nil, fmt.Errorf("none")
}
func (s *memToolStore) GetToolLastSuccessfulTestRunForVersion(_ context.Context, versionID string) (*domain.ToolTestRun, error) {
	for _, runs := range s.testRuns {
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].ToolVersionID == versionID && runs[i].Status == "success" {
				return runs[i], nil
			}
		}
	}
	return nil, fmt.Errorf("none")
}

// fakeSandbox 让 code_execution 路由可测，无需真 docker。
type fakeSandbox struct{}

func (fakeSandbox) Run(_ context.Context, language, code string) (string, error) {
	return fmt.Sprintf("[%s] ran %d bytes", language, len(code)), nil
}

func newToolService(t *testing.T) (*tool.Service, *memToolStore) {
	t.Helper()
	store := newMemToolStore()
	return tool.NewService(store), store
}

func TestRESTExecutor(t *testing.T) {
	// 回显请求体的测试服务器。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"echo":%s}`, string(body))
	}))
	defer srv.Close()

	svc, _ := newToolService(t)
	ctx := context.Background()

	endpoint, _ := json.Marshal(map[string]string{"method": "POST", "url": srv.URL})
	tl, err := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID:    "w1",
		Name:           "echo",
		SourceType:     "rest_api",
		Description:    "回显工具",
		SchemaJSON:     `{"type":"object","properties":{"q":{"type":"string"}}}`,
		EndpointConfig: string(endpoint),
		CreatedBy:      "u1",
	})
	if err != nil {
		t.Fatalf("create tool: %v", err)
	}

	reg := NewRegistry(svc, fakeSandbox{})
	bt, err := reg.Build(ctx, tl.ID)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if bt.Info.Name != "echo" {
		t.Fatalf("unexpected tool name %s", bt.Info.Name)
	}

	out, err := bt.Executor.Execute(ctx, json.RawMessage(`{"q":"hi"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != `{"echo":{"q":"hi"}}` {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestBuildReadsApprovalPresentationFromSchema(t *testing.T) {
	svc, _ := newToolService(t)
	ctx := context.Background()
	tl, err := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "approve_business_action", SourceType: "rest_api",
		Description: "审批业务操作", Sensitive: true,
		SchemaJSON: `{
			"type":"object",
			"properties":{"case_id":{"type":"string"},"amount":{"type":"number"}},
			"x-kbot-approval":{
				"title":"业务审批","operation_label":"批准操作","risk_label":"资金操作",
				"field_order":["case_id","amount"],
				"field_labels":{"case_id":"案件号","amount":"金额"},
				"currency_fields":{"amount":"¥"}
			}
		}`,
		EndpointConfig: `{"method":"POST","url":"http://127.0.0.1/example"}`,
		CreatedBy:      "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	bt, err := NewRegistry(svc, fakeSandbox{}).Build(ctx, tl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bt.ApprovalUI.Title != "业务审批" || bt.ApprovalUI.FieldLabels["case_id"] != "案件号" {
		t.Fatalf("unexpected approval presentation: %#v", bt.ApprovalUI)
	}
	if got := bt.ApprovalUI.CurrencyFields["amount"]; got != "¥" {
		t.Fatalf("unexpected currency symbol %q", got)
	}
}

func TestSDKExecutorRouting(t *testing.T) {
	svc, _ := newToolService(t)
	ctx := context.Background()

	endpoint, _ := json.Marshal(map[string]string{"sdk_name": "my_sdk"})
	tl, _ := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "sdktool", SourceType: "internal_sdk",
		EndpointConfig: string(endpoint), CreatedBy: "u1",
	})

	reg := NewRegistry(svc, fakeSandbox{})
	reg.RegisterSDK("my_sdk", "desc", "", ExecutorFunc(func(_ context.Context, args json.RawMessage) (string, error) {
		return "sdk-ran:" + string(args), nil
	}))

	bt, err := reg.Build(ctx, tl.ID)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := bt.Executor.Execute(ctx, json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != `sdk-ran:{"a":1}` {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestCodeExecutionRouting(t *testing.T) {
	svc, _ := newToolService(t)
	ctx := context.Background()

	endpoint, _ := json.Marshal(map[string]string{"language": "python"})
	tl, _ := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "code_run_python", SourceType: "code_execution",
		EndpointConfig: string(endpoint), CreatedBy: "u1",
	})

	reg := NewRegistry(svc, fakeSandbox{})
	bt, err := reg.Build(ctx, tl.ID)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := bt.Executor.Execute(ctx, json.RawMessage(`{"code":"print(1)"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "[python] ran 8 bytes" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPublishGateRequiresTestRun(t *testing.T) {
	svc, _ := newToolService(t)
	ctx := context.Background()

	tl, _ := svc.CreateTool(ctx, tool.CreateToolRequest{
		WorkspaceID: "w1", Name: "ungated", SourceType: "rest_api",
		EndpointConfig: `{"url":"http://x"}`, CreatedBy: "u1",
	})

	// 没有成功的测试运行 → 不允许发布。
	if err := svc.PublishTool(ctx, tl.ID); err == nil {
		t.Fatal("expected publish to be blocked without a successful test run")
	}

	// 记录一次成功测试后可发布。
	if _, err := svc.RecordTestRun(ctx, tl.ID, "{}", "ok", "success", 10, nil); err != nil {
		t.Fatalf("record test run: %v", err)
	}
	if err := svc.PublishTool(ctx, tl.ID); err != nil {
		t.Fatalf("expected publish to succeed after test run, got %v", err)
	}
}
