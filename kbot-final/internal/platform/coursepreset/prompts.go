// Package coursepreset 提供课堂环境可重复初始化的专业业务 Prompt。
package coursepreset

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/modelconfig"
	"github.com/Q1mi/kbot/internal/platform/prompt"
)

// ScenarioPrompt 描述一个业务场景配套的 System Prompt 与 User Prompt Template。
type ScenarioPrompt struct {
	WorkspaceName       string
	ProfileName         string
	SystemPromptName    string
	UserTemplateName    string
	Category            string
	SystemPrompt        string
	UserTemplate        string
	UserVariablesSchema string
	GenerationConfig    domain.GenerationConfig
	SampleVariables     map[string]any
}

const emptyVariablesSchema = `{"type":"object","additionalProperties":false}`

var (
	//go:embed prompts/crossborder_product_operations.system.md
	crossborderProductSystem string
	//go:embed prompts/crossborder_product_operations.user.md
	crossborderProductUser string
	//go:embed prompts/crossborder_product_operations.schema.json
	crossborderProductSchema string

	//go:embed prompts/crossborder_supply_chain.system.md
	crossborderSupplySystem string
	//go:embed prompts/crossborder_supply_chain.user.md
	crossborderSupplyUser string
	//go:embed prompts/crossborder_supply_chain.schema.json
	crossborderSupplySchema string

	//go:embed prompts/insurance_claim_review.system.md
	insuranceClaimSystem string
	//go:embed prompts/insurance_claim_review.user.md
	insuranceClaimUser string
	//go:embed prompts/insurance_claim_review.schema.json
	insuranceClaimSchema string

	//go:embed prompts/insurance_fraud_analysis.system.md
	insuranceFraudSystem string
	//go:embed prompts/insurance_fraud_analysis.user.md
	insuranceFraudUser string
	//go:embed prompts/insurance_fraud_analysis.schema.json
	insuranceFraudSchema string
)

// ScenarioPrompts 是两个业务 Workspace 的四套课程预设。
var ScenarioPrompts = []ScenarioPrompt{
	{
		WorkspaceName: "跨境电商运营平台", ProfileName: "商品运营 Profile",
		SystemPromptName: "商品运营 · System Prompt", UserTemplateName: "商品运营 · User Prompt Template",
		Category: "crossborder-product-operations", SystemPrompt: crossborderProductSystem,
		UserTemplate: crossborderProductUser, UserVariablesSchema: crossborderProductSchema,
		GenerationConfig: generationConfig(0.2, 0.8, 1800),
		SampleVariables: map[string]any{
			"market": "US", "order_id": "TTS-20260801-1001", "sku": "SKU-BLACK-M-01",
			"objective":      "诊断该商品关联订单的履约风险并给出运营优先级",
			"execution_mode": "analyze_only", "constraints": "优先保障 SLA，控制额外物流成本",
		},
	},
	{
		WorkspaceName: "跨境电商运营平台", ProfileName: "供应链协同 Profile",
		SystemPromptName: "供应链协同 · System Prompt", UserTemplateName: "供应链协同 · User Prompt Template",
		Category: "crossborder-supply-chain", SystemPrompt: crossborderSupplySystem,
		UserTemplate: crossborderSupplyUser, UserVariablesSchema: crossborderSupplySchema,
		GenerationConfig: generationConfig(0.1, 0.7, 2200),
		SampleVariables: map[string]any{
			"task_type": "order_exception", "primary_resource_id": "TTS-20260801-1001",
			"sku": "SKU-BLACK-M-01", "objective": "生成可执行的跨仓调拨恢复方案",
			"execution_mode": "prepare_action", "constraints": "调拨前核验库存和 SLA，写操作等待人工审批",
		},
	},
	{
		WorkspaceName: "保险理赔与反欺诈平台", ProfileName: "理赔审核 Profile",
		SystemPromptName: "理赔审核 · System Prompt", UserTemplateName: "理赔审核 · User Prompt Template",
		Category: "insurance-claim-review", SystemPrompt: insuranceClaimSystem,
		UserTemplate: insuranceClaimUser, UserVariablesSchema: insuranceClaimSchema,
		GenerationConfig: generationConfig(0.1, 0.7, 2400),
		SampleVariables: map[string]any{
			"claim_id": "CLM-2026-0001", "review_goal": "核验责任并给出最高可赔金额",
			"execution_mode": "analyze_only", "operator_instruction": "完整列出规则版本、理由码和金额计算依据",
			"additional_context": "课程标准案件，无额外材料",
		},
	},
	{
		WorkspaceName: "保险理赔与反欺诈平台", ProfileName: "反欺诈分析 Profile",
		SystemPromptName: "反欺诈分析 · System Prompt", UserTemplateName: "反欺诈分析 · User Prompt Template",
		Category: "insurance-fraud-analysis", SystemPrompt: insuranceFraudSystem,
		UserTemplate: insuranceFraudUser, UserVariablesSchema: insuranceFraudSchema,
		GenerationConfig: generationConfig(0.1, 0.6, 2600),
		SampleVariables: map[string]any{
			"claim_id": "CLM-2026-0002", "analysis_goal": "评估欺诈风险并生成调查处置建议",
			"execution_mode": "prepare_action", "investigation_scope": "事故时间、重复图片和关联收款账户",
			"additional_context": "达到高风险阈值时准备冻结与调查操作，等待人工审批",
		},
	},
}

// EnsurePrompts 幂等创建课程 Prompt。System Prompt 固定绑定指定 Profile v1；
// User Prompt Template 保存 Variables Schema，并由课程 Agent 绑定为首轮 user 消息模板。
func EnsurePrompts(
	ctx context.Context,
	iamService *iam.Service,
	modelService *modelconfig.Service,
	promptService *prompt.Service,
) (int, error) {
	workspaces, err := iamService.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		return 0, err
	}
	workspaceByName := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		workspaceByName[workspace.Name] = workspace.ID
	}

	created := 0
	for _, preset := range ScenarioPrompts {
		workspaceID := workspaceByName[preset.WorkspaceName]
		if workspaceID == "" {
			continue
		}
		profileVersionID, err := findProfileV1(ctx, modelService, workspaceID, preset.ProfileName)
		if err != nil {
			return created, err
		}
		if profileVersionID == "" {
			continue
		}
		existing, err := promptService.ListPrompts(ctx, workspaceID)
		if err != nil {
			return created, err
		}
		byName := make(map[string]*domain.Prompt, len(existing))
		for _, item := range existing {
			byName[item.Name] = item
		}
		if _, ok := byName[preset.SystemPromptName]; !ok {
			if _, _, err := promptService.CreatePrompt(ctx, prompt.CreatePromptRequest{
				WorkspaceID: workspaceID, Name: preset.SystemPromptName, Category: preset.Category + "-system",
				Template: preset.SystemPrompt, VariablesSchema: emptyVariablesSchema,
				ModelProfileVersionID: profileVersionID, GenerationConfig: preset.GenerationConfig,
				CreatedBy: "system",
			}); err != nil {
				return created, fmt.Errorf("create course system prompt %q: %w", preset.SystemPromptName, err)
			}
			created++
		}
		if existingPrompt, ok := byName[preset.UserTemplateName]; !ok {
			if _, _, err := promptService.CreatePrompt(ctx, prompt.CreatePromptRequest{
				WorkspaceID: workspaceID, Name: preset.UserTemplateName, Category: preset.Category + "-user-template",
				Template: preset.UserTemplate, VariablesSchema: preset.UserVariablesSchema,
				CreatedBy: "system",
			}); err != nil {
				return created, fmt.Errorf("create course user template %q: %w", preset.UserTemplateName, err)
			}
			created++
		} else {
			updated, err := ensureUserPromptDefaults(ctx, promptService, existingPrompt.ID, preset.SampleVariables)
			if err != nil {
				return created, fmt.Errorf("upgrade course user template %q: %w", preset.UserTemplateName, err)
			}
			if updated {
				created++
			}
		}
	}
	return created, nil
}

// ensureUserPromptDefaults 为既有课程模板补充有效演示数据。它保留当前模板、
// Schema 约束和模型配置，只生成一个新的不可变版本并移动 dev 指针。
func ensureUserPromptDefaults(
	ctx context.Context,
	service *prompt.Service,
	promptID string,
	defaults map[string]any,
) (bool, error) {
	versionID, err := service.ResolveVersion(ctx, promptID, prompt.EnvDev, "system")
	if err != nil {
		return false, err
	}
	current, err := service.GetVersion(ctx, versionID)
	if err != nil {
		return false, err
	}
	schemaWithDefaults, changed, err := mergeSchemaDefaults(current.VariablesSchema, defaults)
	if err != nil || !changed {
		return false, err
	}
	version, err := service.CreateVersionConfigured(
		ctx, promptID, current.Template, schemaWithDefaults,
		current.ModelProfileVersionID, current.GenerationConfig, "system",
	)
	if err != nil {
		return false, err
	}
	if err := service.Promote(ctx, promptID, prompt.EnvDev, version.ID); err != nil {
		return false, err
	}
	return true, nil
}

func mergeSchemaDefaults(raw string, defaults map[string]any) (string, bool, error) {
	var document map[string]any
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return "", false, fmt.Errorf("decode variables schema: %w", err)
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		return "", false, fmt.Errorf("variables schema has no properties")
	}
	changed := false
	for name, value := range defaults {
		property, ok := properties[name].(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("variables schema has no property %q", name)
		}
		if current, exists := property["default"]; exists && reflect.DeepEqual(current, value) {
			continue
		}
		property["default"] = value
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", false, err
	}
	return string(encoded) + "\n", true, nil
}

func findProfileV1(ctx context.Context, service *modelconfig.Service, workspaceID, name string) (string, error) {
	profiles, err := service.ListProfiles(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	for _, profile := range profiles {
		if profile.Name != name {
			continue
		}
		versions, err := service.ListProfileVersions(ctx, profile.ID)
		if err != nil {
			return "", err
		}
		for _, version := range versions {
			if version.Version == 1 {
				return version.ID, nil
			}
		}
	}
	return "", nil
}

func generationConfig(temperature, topP float32, maxTokens int) domain.GenerationConfig {
	return domain.GenerationConfig{
		Temperature: &temperature, TopP: &topP, MaxOutputTokens: &maxTokens,
	}
}
