package coursepreset_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform"
	"github.com/Q1mi/kbot/internal/platform/coursepreset"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/runtime/promptcache"
)

const emptySchema = `{"type":"object","additionalProperties":false}`

func TestScenarioPromptsCompileAndRender(t *testing.T) {
	t.Parallel()

	if len(coursepreset.ScenarioPrompts) != 4 {
		t.Fatalf("scenario prompt count = %d, want 4", len(coursepreset.ScenarioPrompts))
	}
	names := make(map[string]struct{}, len(coursepreset.ScenarioPrompts)*2)
	for _, preset := range coursepreset.ScenarioPrompts {
		preset := preset
		t.Run(preset.ProfileName, func(t *testing.T) {
			for _, name := range []string{preset.SystemPromptName, preset.UserTemplateName} {
				if _, exists := names[name]; exists {
					t.Fatalf("duplicate prompt name %q", name)
				}
				names[name] = struct{}{}
			}

			system, err := promptcache.Compile("system-v1", preset.SystemPrompt, emptySchema)
			if err != nil {
				t.Fatalf("compile system prompt: %v", err)
			}
			if _, err := system.Render(nil); err != nil {
				t.Fatalf("render system prompt: %v", err)
			}

			user, err := promptcache.Compile("user-v1", preset.UserTemplate, preset.UserVariablesSchema)
			if err != nil {
				t.Fatalf("compile user template: %v", err)
			}
			rendered, err := user.Render(preset.SampleVariables)
			if err != nil {
				t.Fatalf("render user template: %v", err)
			}
			if strings.Contains(rendered, "{{") || strings.TrimSpace(rendered) == "" {
				t.Fatalf("user template was not fully rendered: %q", rendered)
			}
			var schemaDocument struct {
				Properties map[string]struct {
					Default any `json:"default"`
				} `json:"properties"`
			}
			if err := json.Unmarshal([]byte(preset.UserVariablesSchema), &schemaDocument); err != nil {
				t.Fatalf("decode user variables schema: %v", err)
			}
			for name, want := range preset.SampleVariables {
				if got := schemaDocument.Properties[name].Default; !reflect.DeepEqual(got, want) {
					t.Errorf("%s default = %#v, want %#v", name, got, want)
				}
			}
		})
	}
}

func TestEnsurePromptsUpgradesMissingCourseDefaults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := []byte("course-default-upgrade-key-32b!")
	services := platform.NewService(nil, nil, key, prompt.NoopPublisher{}, nil, nil, key)
	if err := services.IAM.EnsureSeedWorkspaces(ctx); err != nil {
		t.Fatalf("seed workspaces: %v", err)
	}
	seedModelConfigs(t, ctx, services)
	if _, err := coursepreset.EnsurePrompts(ctx, services.IAM, services.ModelConfig, services.Prompt); err != nil {
		t.Fatalf("ensure prompts: %v", err)
	}

	workspaces, _ := services.IAM.ListWorkspaces(ctx, 200, 0)
	var workspaceID string
	for _, workspace := range workspaces {
		if workspace.Name == "跨境电商运营平台" {
			workspaceID = workspace.ID
		}
	}
	items, err := services.Prompt.ListPrompts(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	var promptID string
	for _, item := range items {
		if item.Name == "商品运营 · User Prompt Template" {
			promptID = item.ID
		}
	}
	currentID, err := services.Prompt.ResolveVersion(ctx, promptID, prompt.EnvDev, "teacher")
	if err != nil {
		t.Fatalf("resolve course prompt: %v", err)
	}
	current, err := services.Prompt.GetVersion(ctx, currentID)
	if err != nil {
		t.Fatalf("get course prompt version: %v", err)
	}
	var legacySchema map[string]any
	if err := json.Unmarshal([]byte(current.VariablesSchema), &legacySchema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	properties := legacySchema["properties"].(map[string]any)
	for _, property := range properties {
		delete(property.(map[string]any), "default")
	}
	rawLegacySchema, _ := json.Marshal(legacySchema)
	legacy, err := services.Prompt.CreateVersionConfigured(
		ctx, promptID, current.Template, string(rawLegacySchema),
		current.ModelProfileVersionID, current.GenerationConfig, "teacher",
	)
	if err != nil {
		t.Fatalf("create legacy version: %v", err)
	}
	if err := services.Prompt.Promote(ctx, promptID, prompt.EnvDev, legacy.ID); err != nil {
		t.Fatalf("promote legacy version: %v", err)
	}

	changed, err := coursepreset.EnsurePrompts(ctx, services.IAM, services.ModelConfig, services.Prompt)
	if err != nil {
		t.Fatalf("upgrade prompts: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	upgradedID, _ := services.Prompt.ResolveVersion(ctx, promptID, prompt.EnvDev, "teacher")
	upgraded, _ := services.Prompt.GetVersion(ctx, upgradedID)
	for name, want := range coursepreset.ScenarioPrompts[0].SampleVariables {
		var document map[string]any
		if err := json.Unmarshal([]byte(upgraded.VariablesSchema), &document); err != nil {
			t.Fatalf("decode upgraded schema: %v", err)
		}
		property := document["properties"].(map[string]any)[name].(map[string]any)
		if got := property["default"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s default = %#v, want %#v", name, got, want)
		}
	}
}

func TestEnsurePromptsIsIdempotentAndBindsProfileV1(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	key := []byte("course-prompt-test-key-32-bytes!")
	services := platform.NewService(nil, nil, key, prompt.NoopPublisher{}, nil, nil, key)
	if err := services.IAM.EnsureSeedWorkspaces(ctx); err != nil {
		t.Fatalf("seed workspaces: %v", err)
	}
	seedModelConfigs(t, ctx, services)

	created, err := coursepreset.EnsurePrompts(ctx, services.IAM, services.ModelConfig, services.Prompt)
	if err != nil {
		t.Fatalf("ensure prompts: %v", err)
	}
	if created != 8 {
		t.Fatalf("created = %d, want 8", created)
	}
	created, err = coursepreset.EnsurePrompts(ctx, services.IAM, services.ModelConfig, services.Prompt)
	if err != nil {
		t.Fatalf("ensure prompts again: %v", err)
	}
	if created != 0 {
		t.Fatalf("created on second run = %d, want 0", created)
	}

	workspaces, err := services.IAM.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	workspaceIDs := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		workspaceIDs[workspace.Name] = workspace.ID
	}

	for _, preset := range coursepreset.ScenarioPrompts {
		workspaceID := workspaceIDs[preset.WorkspaceName]
		profileVersionID := profileV1ID(t, ctx, services, workspaceID, preset.ProfileName)
		items, err := services.Prompt.ListPrompts(ctx, workspaceID)
		if err != nil {
			t.Fatalf("list prompts for %s: %v", preset.WorkspaceName, err)
		}
		byName := make(map[string]string, len(items))
		for _, item := range items {
			byName[item.Name] = item.ID
		}

		systemVersion := promptV1(t, ctx, services, byName[preset.SystemPromptName])
		if systemVersion.ModelProfileVersionID != profileVersionID {
			t.Errorf("%s profile version = %q, want %q", preset.SystemPromptName, systemVersion.ModelProfileVersionID, profileVersionID)
		}
		if systemVersion.GenerationConfig.Temperature == nil || systemVersion.GenerationConfig.MaxOutputTokens == nil {
			t.Errorf("%s generation config is incomplete", preset.SystemPromptName)
		}

		userVersion := promptV1(t, ctx, services, byName[preset.UserTemplateName])
		if userVersion.ModelProfileVersionID != "" {
			t.Errorf("%s should be a render-only teaching asset", preset.UserTemplateName)
		}
	}
}

func seedModelConfigs(t *testing.T, ctx context.Context, services *platform.Service) {
	t.Helper()
	workspaces, err := services.IAM.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	profiles := map[string][]modelconfig.SeedProfile{
		"跨境电商运营平台": {
			{Name: "商品运营 Profile", DeploymentName: "商品运营模型", ClassificationMax: "internal"},
			{Name: "供应链协同 Profile", DeploymentName: "供应链协同模型", ClassificationMax: "confidential"},
		},
		"保险理赔与反欺诈平台": {
			{Name: "理赔审核 Profile", DeploymentName: "理赔审核模型", ClassificationMax: "confidential"},
			{Name: "反欺诈分析 Profile", DeploymentName: "反欺诈分析模型", ClassificationMax: "confidential"},
		},
	}
	providers := map[string]string{
		"跨境电商运营平台":   "电商专用模型账号",
		"保险理赔与反欺诈平台": "保险专用模型账号",
	}
	for _, workspace := range workspaces {
		if err := services.ModelConfig.EnsureSeedWorkspaceConfig(ctx, modelconfig.SeedWorkspaceConfig{
			WorkspaceID: workspace.ID, ProviderName: providers[workspace.Name],
			BaseURL: "http://mock-llm:8090/v1", APIKey: "test-key", ModelName: "mock-agent",
			CreatedBy: "system", Profiles: profiles[workspace.Name],
		}); err != nil {
			t.Fatalf("seed model config for %s: %v", workspace.Name, err)
		}
	}
}

func profileV1ID(t *testing.T, ctx context.Context, services *platform.Service, workspaceID, profileName string) string {
	t.Helper()
	profiles, err := services.ModelConfig.ListProfiles(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	for _, profile := range profiles {
		if profile.Name != profileName {
			continue
		}
		versions, err := services.ModelConfig.ListProfileVersions(ctx, profile.ID)
		if err != nil {
			t.Fatalf("list profile versions: %v", err)
		}
		for _, version := range versions {
			if version.Version == 1 {
				return version.ID
			}
		}
	}
	t.Fatalf("profile v1 %q not found", profileName)
	return ""
}

func promptV1(t *testing.T, ctx context.Context, services *platform.Service, promptID string) *domain.PromptVersion {
	t.Helper()
	if promptID == "" {
		t.Fatal("prompt not found")
	}
	versions, err := services.Prompt.ListVersions(ctx, promptID)
	if err != nil {
		t.Fatalf("list prompt versions: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("prompt versions = %v, want only v1", versions)
	}
	return versions[0]
}
