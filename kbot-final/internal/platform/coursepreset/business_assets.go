package coursepreset

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Q1mi/kbot/internal/domain"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/iam"
	"github.com/Q1mi/kbot/internal/platform/kb"
	"github.com/Q1mi/kbot/internal/platform/prompt"
	"github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/platform/tool"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

const (
	crossborderWorkspace = "跨境电商运营平台"
	insuranceWorkspace   = "保险理赔与反欺诈平台"
)

var (
	//go:embed assets/crossborder.tools.json
	crossborderToolsJSON string
	//go:embed assets/insurance.tools.json
	insuranceToolsJSON string

	//go:embed assets/skills/product-fulfillment-diagnosis/SKILL.md
	productFulfillmentDiagnosisSkill string
	//go:embed assets/skills/order-exception-recovery/SKILL.md
	orderExceptionRecoverySkill string
	//go:embed assets/skills/settlement-reconciliation/SKILL.md
	settlementReconciliationSkill string
	//go:embed assets/skills/claim-coverage-review/SKILL.md
	claimCoverageReviewSkill string
	//go:embed assets/skills/fraud-risk-triage/SKILL.md
	fraudRiskTriageSkill string
	//go:embed assets/skills/fraud-investigation-coordination/SKILL.md
	fraudInvestigationCoordinationSkill string

	//go:embed assets/knowledge
	knowledgeAssets embed.FS
)

// BusinessAssetOptions 配置两个独立业务模拟器的容器内访问地址。
type BusinessAssetOptions struct {
	CrossborderBaseURL string
	InsuranceBaseURL   string
}

// DefaultBusinessAssetOptions 返回完整课堂 Compose 使用的默认服务地址。
func DefaultBusinessAssetOptions() BusinessAssetOptions {
	return BusinessAssetOptions{
		CrossborderBaseURL: "http://crossborder-sim:8091",
		InsuranceBaseURL:   "http://insurance-sim:8092",
	}
}

// AssetCounts 记录本次幂等初始化实际新建的资源数量。
type AssetCounts struct {
	KnowledgeBases int
	Documents      int
	Tools          int
	Skills         int
	SkillVersions  int
	Agents         int
	AgentVersions  int
}

type toolPreset struct {
	Name           string          `json:"name"`
	SourceType     string          `json:"source_type"`
	Description    string          `json:"description"`
	Sensitive      bool            `json:"sensitive"`
	SchemaJSON     json.RawMessage `json:"schema_json"`
	EndpointPath   string          `json:"endpoint_path"`
	EndpointConfig json.RawMessage `json:"-"`
	TestInput      json.RawMessage `json:"test_input"`
	BaseURL        string          `json:"-"`
}

type knowledgeBasePreset struct {
	WorkspaceName string
	Name          string
	Directory     string
}

type skillPreset struct {
	WorkspaceName     string
	Category          string
	SkillMD           string
	KnowledgeBaseName string
}

type agentPreset struct {
	WorkspaceName     string
	Name              string
	Template          string
	SystemPromptName  string
	UserPromptName    string
	ToolNames         []string
	SkillNames        []string
	KnowledgeBaseName string
	MaxSteps          int
}

var knowledgeBasePresets = []knowledgeBasePreset{
	{crossborderWorkspace, "跨境电商商品运营知识库", "assets/knowledge/crossborder-product"},
	{crossborderWorkspace, "跨境电商供应链协同知识库", "assets/knowledge/crossborder-supply-chain"},
	{insuranceWorkspace, "保险理赔审核知识库", "assets/knowledge/insurance-claim-review"},
	{insuranceWorkspace, "保险反欺诈分析知识库", "assets/knowledge/insurance-fraud-analysis"},
}

var skillPresets = []skillPreset{
	{crossborderWorkspace, "crossborder-product-operations", productFulfillmentDiagnosisSkill, "跨境电商商品运营知识库"},
	{crossborderWorkspace, "crossborder-supply-chain", orderExceptionRecoverySkill, "跨境电商供应链协同知识库"},
	{crossborderWorkspace, "crossborder-settlement", settlementReconciliationSkill, "跨境电商供应链协同知识库"},
	{insuranceWorkspace, "insurance-claim-review", claimCoverageReviewSkill, "保险理赔审核知识库"},
	{insuranceWorkspace, "insurance-fraud-analysis", fraudRiskTriageSkill, "保险反欺诈分析知识库"},
	{insuranceWorkspace, "insurance-fraud-investigation", fraudInvestigationCoordinationSkill, "保险反欺诈分析知识库"},
}

var agentPresets = []agentPreset{
	{
		WorkspaceName: crossborderWorkspace, Name: "商品运营 Agent", Template: "crossborder_product_operations",
		SystemPromptName: "商品运营 · System Prompt", UserPromptName: "商品运营 · User Prompt Template",
		ToolNames:  []string{"get_order", "get_inventory", "get_shipping_options", "create_inventory_transfer", "approve_refund", "search_knowledge_base"},
		SkillNames: []string{"product_fulfillment_diagnosis"}, KnowledgeBaseName: "跨境电商商品运营知识库", MaxSteps: 8,
	},
	{
		WorkspaceName: crossborderWorkspace, Name: "供应链协同 Agent", Template: "crossborder_supply_chain",
		SystemPromptName: "供应链协同 · System Prompt", UserPromptName: "供应链协同 · User Prompt Template",
		ToolNames:  []string{"get_order", "get_inventory", "get_shipping_options", "get_statement", "create_inventory_transfer", "approve_refund", "create_reconciliation_case", "search_knowledge_base"},
		SkillNames: []string{"order_exception_recovery", "settlement_reconciliation"}, KnowledgeBaseName: "跨境电商供应链协同知识库", MaxSteps: 12,
	},
	{
		WorkspaceName: insuranceWorkspace, Name: "理赔审核 Agent", Template: "insurance_claim_review",
		SystemPromptName: "理赔审核 · System Prompt", UserPromptName: "理赔审核 · User Prompt Template",
		ToolNames:  []string{"get_claim", "get_policy", "evaluate_coverage", "get_fraud_features", "request_additional_documents", "approve_claim", "search_knowledge_base"},
		SkillNames: []string{"claim_coverage_review"}, KnowledgeBaseName: "保险理赔审核知识库", MaxSteps: 10,
	},
	{
		WorkspaceName: insuranceWorkspace, Name: "反欺诈分析 Agent", Template: "insurance_fraud_analysis",
		SystemPromptName: "反欺诈分析 · System Prompt", UserPromptName: "反欺诈分析 · User Prompt Template",
		ToolNames:  []string{"get_claim", "get_policy", "evaluate_coverage", "get_fraud_features", "hold_claim_payment", "open_fraud_investigation", "search_knowledge_base"},
		SkillNames: []string{"fraud_risk_triage", "fraud_investigation_coordination"}, KnowledgeBaseName: "保险反欺诈分析知识库", MaxSteps: 10,
	},
}

// EnsureBusinessAssets 幂等创建并装配课程 Tool、Skill 与 Agent。
// 新 Tool 通过真实业务模拟器完成试调后发布，敏感写操作统一使用 dry_run 样例。
func EnsureBusinessAssets(
	ctx context.Context,
	iamService *iam.Service,
	promptService *prompt.Service,
	kbService *kb.Service,
	toolService *tool.Service,
	skillService *skill.Service,
	agentService *agent.Service,
	registry *tooling.Registry,
	options BusinessAssetOptions,
) (AssetCounts, error) {
	var counts AssetCounts
	if strings.TrimRight(options.CrossborderBaseURL, "/") == "" || strings.TrimRight(options.InsuranceBaseURL, "/") == "" {
		return counts, fmt.Errorf("business simulator base URLs are required")
	}
	workspaceIDs, err := workspaceIDsByName(ctx, iamService)
	if err != nil {
		return counts, err
	}
	knowledgeBaseIDs, createdKBs, ingestedDocuments, err := ensureKnowledgeBases(ctx, workspaceIDs, kbService)
	if err != nil {
		return counts, err
	}
	counts.KnowledgeBases += createdKBs
	counts.Documents += ingestedDocuments

	toolIDs := make(map[string]map[string]string)
	sets := []struct {
		workspaceName string
		baseURL       string
		raw           string
	}{
		{crossborderWorkspace, options.CrossborderBaseURL, crossborderToolsJSON},
		{insuranceWorkspace, options.InsuranceBaseURL, insuranceToolsJSON},
	}
	for _, set := range sets {
		workspaceID := workspaceIDs[set.workspaceName]
		if workspaceID == "" {
			continue
		}
		presets, err := parseToolPresets(set.raw, set.baseURL)
		if err != nil {
			return counts, fmt.Errorf("parse tools for %s: %w", set.workspaceName, err)
		}
		searchPreset, err := knowledgeSearchToolPreset(knowledgeBaseIDs[set.workspaceName])
		if err != nil {
			return counts, fmt.Errorf("build knowledge search tool for %s: %w", set.workspaceName, err)
		}
		presets = append(presets, searchPreset)
		ids, created, err := ensureTools(ctx, workspaceID, presets, toolService, registry)
		if err != nil {
			return counts, fmt.Errorf("ensure tools for %s: %w", set.workspaceName, err)
		}
		toolIDs[set.workspaceName] = ids
		counts.Tools += created
	}

	skillVersionIDs, createdSkills, createdSkillVersions, err := ensureSkills(ctx, workspaceIDs, knowledgeBaseIDs, skillService)
	if err != nil {
		return counts, err
	}
	counts.Skills += createdSkills
	counts.SkillVersions += createdSkillVersions

	createdAgents, createdAgentVersions, err := ensureAgents(
		ctx, workspaceIDs, knowledgeBaseIDs, toolIDs, skillVersionIDs, promptService, agentService,
	)
	if err != nil {
		return counts, err
	}
	counts.Agents += createdAgents
	counts.AgentVersions += createdAgentVersions
	return counts, nil
}

func workspaceIDsByName(ctx context.Context, service *iam.Service) (map[string]string, error) {
	workspaces, err := service.ListWorkspaces(ctx, 200, 0)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		ids[workspace.Name] = workspace.ID
	}
	return ids, nil
}

func ensureKnowledgeBases(
	ctx context.Context,
	workspaceIDs map[string]string,
	service *kb.Service,
) (map[string]map[string]string, int, int, error) {
	ids := make(map[string]map[string]string)
	created := 0
	ingested := 0
	for _, preset := range knowledgeBasePresets {
		workspaceID := workspaceIDs[preset.WorkspaceName]
		if workspaceID == "" {
			continue
		}
		if ids[preset.WorkspaceName] == nil {
			ids[preset.WorkspaceName] = make(map[string]string)
		}

		bases, err := service.ListKBs(ctx, workspaceID)
		if err != nil {
			return nil, created, ingested, fmt.Errorf("list knowledge bases for %s: %w", preset.WorkspaceName, err)
		}
		var baseID string
		for _, base := range bases {
			if base.Name == preset.Name {
				baseID = base.ID
				break
			}
		}
		if baseID == "" {
			base, err := service.CreateKB(ctx, kb.CreateKBRequest{
				WorkspaceID: workspaceID, Name: preset.Name,
				EmbeddingModel: "text-embedding-3-small", CreatedBy: "system",
			})
			if err != nil {
				return nil, created, ingested, fmt.Errorf("create knowledge base %q: %w", preset.Name, err)
			}
			baseID = base.ID
			created++
		}
		ids[preset.WorkspaceName][preset.Name] = baseID

		documents, err := embeddedKnowledgeDocuments(preset.Directory)
		if err != nil {
			return nil, created, ingested, fmt.Errorf("load knowledge base %q: %w", preset.Name, err)
		}
		result, err := service.SyncStaticDocuments(ctx, baseID, documents)
		if err != nil {
			return nil, created, ingested, fmt.Errorf("sync knowledge base %q: %w", preset.Name, err)
		}
		ingested += result.Ingested
	}
	return ids, created, ingested, nil
}

func embeddedKnowledgeDocuments(directory string) ([]kb.StaticDocument, error) {
	entries, err := fs.ReadDir(knowledgeAssets, directory)
	if err != nil {
		return nil, err
	}
	documents := make([]kb.StaticDocument, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".md" {
			continue
		}
		assetPath := path.Join(directory, entry.Name())
		content, err := knowledgeAssets.ReadFile(assetPath)
		if err != nil {
			return nil, err
		}
		title := firstMarkdownHeading(string(content))
		if title == "" {
			title = strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
		}
		documents = append(documents, kb.StaticDocument{
			SourceURI: "coursepreset://" + strings.TrimPrefix(assetPath, "assets/knowledge/"),
			Title:     title,
			Content:   string(content),
		})
	}
	if len(documents) == 0 {
		return nil, fmt.Errorf("no Markdown documents found in %s", directory)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].SourceURI < documents[j].SourceURI })
	return documents, nil
}

func firstMarkdownHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func knowledgeSearchToolPreset(baseIDs map[string]string) (toolPreset, error) {
	names := make([]string, 0, len(baseIDs))
	for name := range baseIDs {
		names = append(names, name)
	}
	if len(names) == 0 {
		return toolPreset{}, fmt.Errorf("workspace has no course knowledge base")
	}
	sort.Strings(names)
	testInput, err := json.Marshal(map[string]any{
		"kb_id": baseIDs[names[0]], "query": "查询当前场景的关键业务规则和转人工条件", "top_k": 3,
	})
	if err != nil {
		return toolPreset{}, err
	}
	return toolPreset{
		Name:        "search_knowledge_base",
		SourceType:  "internal_sdk",
		Description: "在当前 Agent 获准访问的业务知识库中执行关键词与向量混合检索，返回带来源的规则片段。",
		SchemaJSON: json.RawMessage(`{
  "type":"object",
  "properties":{
    "kb_id":{"type":"string","description":"Agent 获准访问的知识库 ID"},
    "query":{"type":"string","minLength":1,"description":"业务问题、规则或关键词"},
    "top_k":{"type":"integer","minimum":1,"maximum":10,"default":5}
  },
  "required":["kb_id","query"]
}`),
		EndpointConfig: json.RawMessage(`{"sdk_name":"search_knowledge_base"}`),
		TestInput:      testInput,
	}, nil
}

func parseToolPresets(raw, baseURL string) ([]toolPreset, error) {
	var presets []toolPreset
	if err := json.Unmarshal([]byte(raw), &presets); err != nil {
		return nil, err
	}
	baseURL = strings.TrimRight(baseURL, "/")
	for i := range presets {
		presets[i].BaseURL = baseURL
		if presets[i].Name == "" || presets[i].EndpointPath == "" || len(presets[i].SchemaJSON) == 0 || len(presets[i].TestInput) == 0 {
			return nil, fmt.Errorf("tool preset at index %d is incomplete", i)
		}
	}
	return presets, nil
}

func ensureTools(
	ctx context.Context,
	workspaceID string,
	presets []toolPreset,
	service *tool.Service,
	registry *tooling.Registry,
) (map[string]string, int, error) {
	existing, err := service.ListTools(ctx, workspaceID)
	if err != nil {
		return nil, 0, err
	}
	byName := make(map[string]*struct {
		id        string
		createdBy string
	}, len(existing))
	for _, item := range existing {
		byName[item.Name] = &struct {
			id        string
			createdBy string
		}{item.ID, item.CreatedBy}
	}

	ids := make(map[string]string, len(presets))
	created := 0
	for _, preset := range presets {
		entry := byName[preset.Name]
		createdNow := false
		if entry == nil {
			endpoint := preset.EndpointConfig
			if len(endpoint) == 0 {
				endpoint, err = json.Marshal(map[string]any{
					"method": "POST", "url": preset.BaseURL + preset.EndpointPath, "timeout_ms": 5000,
				})
				if err != nil {
					return nil, created, err
				}
			}
			item, err := service.CreateTool(ctx, tool.CreateToolRequest{
				WorkspaceID: workspaceID, Name: preset.Name, SourceType: preset.SourceType,
				Description: preset.Description, SchemaJSON: string(preset.SchemaJSON),
				EndpointConfig: string(endpoint), AuthConfig: "{}", Sensitive: preset.Sensitive,
				CreatedBy: "system",
			})
			if err != nil {
				return nil, created, fmt.Errorf("create tool %q: %w", preset.Name, err)
			}
			entry = &struct {
				id        string
				createdBy string
			}{item.ID, item.CreatedBy}
			byName[preset.Name] = entry
			created++
			createdNow = true
		}
		ids[preset.Name] = entry.id

		version, err := service.GetToolCurrentVersion(ctx, entry.id)
		if err != nil {
			return nil, created, err
		}
		if version.Status == "published" {
			continue
		}
		if version.Status != "draft" {
			return nil, created, fmt.Errorf("tool %q has unsupported status %q", preset.Name, version.Status)
		}
		if !createdNow && entry.createdBy != "system" {
			return nil, created, fmt.Errorf("tool %q is an unpublished user resource; publish it before course initialization", preset.Name)
		}
		if err := testAndPublishTool(ctx, entry.id, preset.TestInput, service, registry); err != nil {
			return nil, created, fmt.Errorf("preflight tool %q: %w", preset.Name, err)
		}
	}
	return ids, created, nil
}

func testAndPublishTool(
	ctx context.Context,
	toolID string,
	input json.RawMessage,
	service *tool.Service,
	registry *tooling.Registry,
) error {
	built, err := registry.Build(ctx, toolID)
	if err != nil {
		return err
	}
	started := time.Now()
	output, executeErr := built.Executor.Execute(ctx, input)
	status := "success"
	if executeErr != nil {
		status = "error"
	}
	if _, err := service.RecordTestRun(ctx, toolID, string(input), output, status, int(time.Since(started).Milliseconds()), executeErr); err != nil {
		return err
	}
	if executeErr != nil {
		return executeErr
	}
	return service.PublishTool(ctx, toolID)
}

func ensureSkills(
	ctx context.Context,
	workspaceIDs map[string]string,
	knowledgeBaseIDs map[string]map[string]string,
	service *skill.Service,
) (map[string]map[string]string, int, int, error) {
	versionsByWorkspace := make(map[string]map[string]string)
	created := 0
	createdVersions := 0
	for _, preset := range skillPresets {
		workspaceID := workspaceIDs[preset.WorkspaceName]
		if workspaceID == "" {
			continue
		}
		baseID := knowledgeBaseIDs[preset.WorkspaceName][preset.KnowledgeBaseName]
		if baseID == "" {
			return nil, created, createdVersions, fmt.Errorf("knowledge base %q not found for skill", preset.KnowledgeBaseName)
		}
		desiredSkillMD := strings.ReplaceAll(preset.SkillMD, "__KB_ID__", baseID)
		frontmatter, desiredBody, err := skill.ParseSkill(desiredSkillMD)
		if err != nil {
			return nil, created, createdVersions, fmt.Errorf("parse skill for %s: %w", preset.WorkspaceName, err)
		}
		items, err := service.ListSkills(ctx, workspaceID)
		if err != nil {
			return nil, created, createdVersions, err
		}
		var currentSkillID string
		for _, item := range items {
			if item.Name == frontmatter.Name {
				currentSkillID = item.ID
				break
			}
		}

		var versionID string
		if currentSkillID == "" {
			_, version, err := service.CreateSkill(ctx, skill.CreateSkillRequest{
				WorkspaceID: workspaceID, Category: preset.Category, SkillMD: desiredSkillMD, CreatedBy: "system",
			})
			if err != nil {
				return nil, created, createdVersions, fmt.Errorf("create skill %q: %w", frontmatter.Name, err)
			}
			if err := service.Publish(ctx, version.ID); err != nil {
				return nil, created, createdVersions, fmt.Errorf("publish skill %q: %w", frontmatter.Name, err)
			}
			versionID = version.ID
			created++
		} else {
			versions, err := service.ListVersions(ctx, currentSkillID, workspaceID)
			if err != nil {
				return nil, created, createdVersions, err
			}
			if len(versions) == 0 {
				return nil, created, createdVersions, fmt.Errorf("skill %q has no version", frontmatter.Name)
			}
			latest := versions[len(versions)-1]
			if latest.CreatedBy == "system" {
				same, err := skillVersionMatches(latest, frontmatter, desiredBody)
				if err != nil {
					return nil, created, createdVersions, fmt.Errorf("compare skill %q: %w", frontmatter.Name, err)
				}
				if !same {
					latest, err = service.CreateVersion(ctx, currentSkillID, desiredSkillMD, "system")
					if err != nil {
						return nil, created, createdVersions, fmt.Errorf("upgrade skill %q: %w", frontmatter.Name, err)
					}
					createdVersions++
				}
				if latest.Status == "draft" {
					if err := service.Publish(ctx, latest.ID); err != nil {
						return nil, created, createdVersions, fmt.Errorf("publish skill %q: %w", frontmatter.Name, err)
					}
				}
				versionID = latest.ID
			} else {
				for i := len(versions) - 1; i >= 0; i-- {
					if versions[i].Status == "published" {
						versionID = versions[i].ID
						break
					}
				}
				if versionID == "" {
					return nil, created, createdVersions, fmt.Errorf("user-managed skill %q has no published version", frontmatter.Name)
				}
			}
		}
		if versionsByWorkspace[preset.WorkspaceName] == nil {
			versionsByWorkspace[preset.WorkspaceName] = make(map[string]string)
		}
		versionsByWorkspace[preset.WorkspaceName][frontmatter.Name] = versionID
	}
	return versionsByWorkspace, created, createdVersions, nil
}

func skillVersionMatches(version *domain.SkillVersion, desired skill.Frontmatter, desiredBody string) (bool, error) {
	var current skill.Frontmatter
	if err := json.Unmarshal([]byte(version.FrontmatterJSON), &current); err != nil {
		return false, err
	}
	return reflect.DeepEqual(current, desired) && version.BodyMD == desiredBody, nil
}

func ensureAgents(
	ctx context.Context,
	workspaceIDs map[string]string,
	knowledgeBaseIDs map[string]map[string]string,
	toolIDs map[string]map[string]string,
	skillVersionIDs map[string]map[string]string,
	promptService *prompt.Service,
	service *agent.Service,
) (int, int, error) {
	created := 0
	createdVersions := 0
	allowNetwork := true
	for _, preset := range agentPresets {
		workspaceID := workspaceIDs[preset.WorkspaceName]
		if workspaceID == "" {
			continue
		}
		prompts, err := promptService.ListPrompts(ctx, workspaceID)
		if err != nil {
			return created, createdVersions, err
		}
		var systemPromptID, userPromptID string
		for _, item := range prompts {
			if item.Name == preset.SystemPromptName {
				systemPromptID = item.ID
			}
			if item.Name == preset.UserPromptName {
				userPromptID = item.ID
			}
		}
		if systemPromptID == "" {
			return created, createdVersions, fmt.Errorf("system prompt %q not found", preset.SystemPromptName)
		}
		if userPromptID == "" {
			return created, createdVersions, fmt.Errorf("user prompt template %q not found", preset.UserPromptName)
		}

		attachedTools := make([]string, 0, len(preset.ToolNames))
		for _, name := range preset.ToolNames {
			id := toolIDs[preset.WorkspaceName][name]
			if id == "" {
				return created, createdVersions, fmt.Errorf("tool %q not found for agent %q", name, preset.Name)
			}
			attachedTools = append(attachedTools, id)
		}
		attachedSkills := make([]string, 0, len(preset.SkillNames))
		for _, name := range preset.SkillNames {
			id := skillVersionIDs[preset.WorkspaceName][name]
			if id == "" {
				return created, createdVersions, fmt.Errorf("skill %q not found for agent %q", name, preset.Name)
			}
			attachedSkills = append(attachedSkills, id)
		}
		baseID := knowledgeBaseIDs[preset.WorkspaceName][preset.KnowledgeBaseName]
		if baseID == "" {
			return created, createdVersions, fmt.Errorf("knowledge base %q not found for agent %q", preset.KnowledgeBaseName, preset.Name)
		}
		desired := agent.AgentVersionConfig{
			SystemPromptID: systemPromptID, UserPromptID: userPromptID, PromptEnv: prompt.EnvDev,
			ToolIDs: attachedTools, SkillVersionIDs: attachedSkills, KBIDs: []string{baseID},
			AllowNetwork: &allowNetwork, MaxSteps: preset.MaxSteps,
		}

		existing, err := service.ListAgents(ctx, workspaceID)
		if err != nil {
			return created, createdVersions, err
		}
		var currentAgent *domain.Agent
		for _, item := range existing {
			if item.Name == preset.Name {
				currentAgent = item
				break
			}
		}
		if currentAgent == nil {
			if _, err := service.CreateAgent(ctx, agent.CreateAgentRequest{
				WorkspaceID: workspaceID, Name: preset.Name, Template: preset.Template,
				SystemPromptID: systemPromptID, UserPromptID: userPromptID, PromptEnv: prompt.EnvDev,
				ToolIDs: attachedTools, SkillVersionIDs: attachedSkills, KBIDs: []string{baseID},
				AllowNetwork: &allowNetwork, MaxSteps: preset.MaxSteps, CreatedBy: "system",
			}); err != nil {
				return created, createdVersions, fmt.Errorf("create agent %q: %w", preset.Name, err)
			}
			created++
			continue
		}

		versions, err := service.ListAgentVersions(ctx, currentAgent.ID, workspaceID)
		if err != nil {
			return created, createdVersions, err
		}
		currentVersion := currentDevAgentVersion(versions)
		if currentVersion == nil {
			return created, createdVersions, fmt.Errorf("agent %q has no dev version", preset.Name)
		}
		if currentVersion.CreatedBy != "system" || agentConfigMatches(currentVersion.Config, desired) {
			continue
		}
		if _, err := service.CreateAgentVersion(ctx, currentAgent.ID, workspaceID, desired, "system"); err != nil {
			return created, createdVersions, fmt.Errorf("upgrade agent %q: %w", preset.Name, err)
		}
		createdVersions++
	}
	return created, createdVersions, nil
}

func currentDevAgentVersion(versions []*agent.AgentVersionView) *agent.AgentVersionView {
	for _, version := range versions {
		for _, environment := range version.Environments {
			if environment == prompt.EnvDev {
				return version
			}
		}
	}
	if len(versions) > 0 {
		return versions[0]
	}
	return nil
}

func agentConfigMatches(current, desired agent.AgentVersionConfig) bool {
	return current.SystemPrompt == desired.SystemPrompt &&
		current.SystemPromptVersionID == desired.SystemPromptVersionID &&
		current.SystemPromptID == desired.SystemPromptID &&
		current.UserPromptID == desired.UserPromptID &&
		current.PromptEnv == desired.PromptEnv &&
		sameStringSet(current.ToolIDs, desired.ToolIDs) &&
		sameStringSet(current.SkillVersionIDs, desired.SkillVersionIDs) &&
		sameStringSet(current.KBIDs, desired.KBIDs) &&
		boolValue(current.AllowNetwork, true) == boolValue(desired.AllowNetwork, true) &&
		current.MaxSteps == desired.MaxSteps
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
