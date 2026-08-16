package coursepreset_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/coursepreset"
	"github.com/Q1mi/kbot/internal/platform/prompt"
)

func TestEnsureBusinessAssetsCreatesPublishedIndependentScenarios(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	simulator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid input", http.StatusBadRequest)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"validated"}`))
	}))
	defer simulator.Close()

	ctx := context.Background()
	key := []byte("course-assets-test-key-32-bytes!")
	services := platform.NewService(nil, nil, key, prompt.NoopPublisher{}, nil, nil, key)
	if err := services.IAM.EnsureSeedWorkspaces(ctx); err != nil {
		t.Fatalf("seed workspaces: %v", err)
	}
	seedModelConfigs(t, ctx, services)
	if _, err := coursepreset.EnsurePrompts(ctx, services.IAM, services.ModelConfig, services.Prompt); err != nil {
		t.Fatalf("seed prompts: %v", err)
	}

	options := coursepreset.BusinessAssetOptions{
		CrossborderBaseURL: simulator.URL,
		InsuranceBaseURL:   simulator.URL,
	}
	counts, err := coursepreset.EnsureBusinessAssets(
		ctx, services.IAM, services.Prompt, services.KB, services.Tool, services.Skill,
		services.Agent, services.Registry, options,
	)
	if err != nil {
		t.Fatalf("ensure business assets: %v", err)
	}
	if counts != (coursepreset.AssetCounts{KnowledgeBases: 4, Documents: 8, Tools: 17, Skills: 6, Agents: 4}) {
		t.Fatalf("counts = %+v", counts)
	}
	if calls.Load() != 15 {
		t.Fatalf("tool preflight calls = %d, want 15", calls.Load())
	}

	assertWorkspaceAssets(t, ctx, services, "跨境电商运营平台", 8, 3, 2, map[string][3]int{
		"商品运营 Agent":  {6, 1, 1},
		"供应链协同 Agent": {8, 2, 1},
	})
	assertWorkspaceAssets(t, ctx, services, "保险理赔与反欺诈平台", 9, 3, 2, map[string][3]int{
		"理赔审核 Agent":  {7, 1, 1},
		"反欺诈分析 Agent": {7, 2, 1},
	})

	counts, err = coursepreset.EnsureBusinessAssets(
		ctx, services.IAM, services.Prompt, services.KB, services.Tool, services.Skill,
		services.Agent, services.Registry, options,
	)
	if err != nil {
		t.Fatalf("ensure business assets again: %v", err)
	}
	if counts != (coursepreset.AssetCounts{}) {
		t.Fatalf("second counts = %+v, want zero", counts)
	}
	if calls.Load() != 15 {
		t.Fatalf("published tools should not be tested again; calls = %d", calls.Load())
	}

	workspaces, err := services.IAM.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	var crossborderID string
	for _, workspace := range workspaces {
		if workspace.Name == "跨境电商运营平台" {
			crossborderID = workspace.ID
			break
		}
	}
	skills, err := services.Skill.ListSkills(ctx, crossborderID)
	if err != nil {
		t.Fatal(err)
	}
	var productSkillID string
	for _, item := range skills {
		if item.Name == "product_fulfillment_diagnosis" {
			productSkillID = item.ID
			break
		}
	}
	legacy, err := services.Skill.CreateVersion(ctx, productSkillID, `---
name: product_fulfillment_diagnosis
description: 旧课程流程
allowed-tools:
  - get_order
requires_network: true
---

# 旧流程

仅查询订单。`, "system")
	if err != nil {
		t.Fatalf("create legacy system skill version: %v", err)
	}
	if err := services.Skill.Publish(ctx, legacy.ID); err != nil {
		t.Fatalf("publish legacy system skill version: %v", err)
	}

	counts, err = coursepreset.EnsureBusinessAssets(
		ctx, services.IAM, services.Prompt, services.KB, services.Tool, services.Skill,
		services.Agent, services.Registry, options,
	)
	if err != nil {
		t.Fatalf("upgrade system-managed assets: %v", err)
	}
	if counts != (coursepreset.AssetCounts{SkillVersions: 1, AgentVersions: 1}) {
		t.Fatalf("upgrade counts = %+v", counts)
	}

	counts, err = coursepreset.EnsureBusinessAssets(
		ctx, services.IAM, services.Prompt, services.KB, services.Tool, services.Skill,
		services.Agent, services.Registry, options,
	)
	if err != nil {
		t.Fatalf("ensure upgraded assets again: %v", err)
	}
	if counts != (coursepreset.AssetCounts{}) {
		t.Fatalf("post-upgrade counts = %+v, want zero", counts)
	}
}

func assertWorkspaceAssets(
	t *testing.T,
	ctx context.Context,
	services *platform.Service,
	workspaceName string,
	wantTools, wantSkills, wantKBs int,
	wantAgents map[string][3]int,
) {
	t.Helper()
	workspaces, err := services.IAM.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	var workspaceID string
	for _, workspace := range workspaces {
		if workspace.Name == workspaceName {
			workspaceID = workspace.ID
			break
		}
	}
	if workspaceID == "" {
		t.Fatalf("workspace %q not found", workspaceName)
	}

	tools, err := services.Tool.ListTools(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != wantTools {
		t.Errorf("%s tools = %d, want %d", workspaceName, len(tools), wantTools)
	}

	knowledgeBases, err := services.KB.ListKBs(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list knowledge bases: %v", err)
	}
	if len(knowledgeBases) != wantKBs {
		t.Errorf("%s knowledge bases = %d, want %d", workspaceName, len(knowledgeBases), wantKBs)
	}
	for _, base := range knowledgeBases {
		documents, err := services.KB.ListDocuments(ctx, base.ID)
		if err != nil {
			t.Fatalf("list knowledge base documents: %v", err)
		}
		if len(documents) != 2 {
			t.Errorf("knowledge base %s documents = %d, want 2", base.Name, len(documents))
		}
		for _, document := range documents {
			if document.Status != "processed" {
				t.Errorf("knowledge document %s status = %s", document.SourceURI, document.Status)
			}
		}
	}
	for _, item := range tools {
		version, err := services.Tool.GetToolCurrentVersion(ctx, item.ID)
		if err != nil {
			t.Fatalf("get tool version: %v", err)
		}
		if version.Status != "published" {
			t.Errorf("tool %s status = %s", item.Name, version.Status)
		}
	}

	skills, err := services.Skill.ListSkills(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != wantSkills {
		t.Errorf("%s skills = %d, want %d", workspaceName, len(skills), wantSkills)
	}
	for _, item := range skills {
		versions, err := services.Skill.ListVersions(ctx, item.ID, workspaceID)
		if err != nil {
			t.Fatalf("list skill versions: %v", err)
		}
		if len(versions) != 1 || versions[0].Status != "published" {
			t.Errorf("skill %s versions = %+v", item.Name, versions)
		}
	}

	agents, err := services.Agent.ListAgents(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != len(wantAgents) {
		t.Errorf("%s agents = %d, want %d", workspaceName, len(agents), len(wantAgents))
	}
	for _, item := range agents {
		want, ok := wantAgents[item.Name]
		if !ok {
			t.Errorf("unexpected agent %s", item.Name)
			continue
		}
		versions, err := services.Agent.ListAgentVersions(ctx, item.ID, workspaceID)
		if err != nil {
			t.Fatalf("list agent versions: %v", err)
		}
		if len(versions) != 1 {
			t.Fatalf("agent %s version count = %d", item.Name, len(versions))
		}
		cfg := versions[0].Config
		if len(cfg.ToolIDs) != want[0] || len(cfg.SkillVersionIDs) != want[1] || len(cfg.KBIDs) != want[2] {
			t.Errorf("agent %s tools/skills/kbs = %d/%d/%d, want %d/%d/%d", item.Name,
				len(cfg.ToolIDs), len(cfg.SkillVersionIDs), len(cfg.KBIDs), want[0], want[1], want[2])
		}
		if cfg.SystemPromptID == "" || cfg.UserPromptID == "" || cfg.PromptEnv != prompt.EnvDev {
			t.Errorf("agent %s prompt binding is incomplete: %+v", item.Name, cfg)
		}
		if cfg.AllowNetwork == nil || !*cfg.AllowNetwork {
			t.Errorf("agent %s must allow its REST tools", item.Name)
		}
		if len(versions[0].Environments) != 1 || versions[0].Environments[0] != "dev" {
			t.Errorf("agent %s dev binding = %v", item.Name, versions[0].Environments)
		}
	}
}
