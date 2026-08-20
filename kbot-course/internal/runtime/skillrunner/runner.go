// Package skillrunner 把平台固定版本的 Skill 适配到 Eino Skill middleware。
package skillrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/schema"

	platformskill "github.com/Q1mi/kbot/internal/platform/skill"
	"github.com/Q1mi/kbot/internal/runtime/tooling"
)

// Backend 把会话已固定的 Skill 版本适配为 Eino 渐进式披露后端。
type Backend struct{ packages []platformskill.Package }

func NewBackend(packages []platformskill.Package) *Backend {
	return &Backend{packages: append([]platformskill.Package(nil), packages...)}
}

func (b *Backend) List(context.Context) ([]einoskill.FrontMatter, error) {
	result := make([]einoskill.FrontMatter, 0, len(b.packages))
	for _, pkg := range b.packages {
		result = append(result, einoskill.FrontMatter{Name: pkg.Name, Description: pkg.Description})
	}
	return result, nil
}

func (b *Backend) Get(_ context.Context, name string) (einoskill.Skill, error) {
	pkg, ok := Find(b.packages, name)
	if !ok {
		return einoskill.Skill{}, fmt.Errorf("skill %q is not available", name)
	}
	return einoskill.Skill{
		FrontMatter: einoskill.FrontMatter{Name: pkg.Name, Description: pkg.Description},
		Content:     pkg.Instructions,
	}, nil
}

// Runtime 汇总 Eino handlers 与工具执行前的 Skill 权限检查。
type Runtime struct {
	Handlers     []adk.ChatModelAgentMiddleware
	ExplicitName string
	policy       *policy
}

func (r *Runtime) Authorize(toolName, arguments string) error {
	if r == nil || r.policy == nil {
		return nil
	}
	return r.policy.Authorize(toolName, arguments)
}

func (r *Runtime) ActiveName() string {
	if r == nil || r.policy == nil {
		return ""
	}
	active := r.policy.Active()
	if active == nil {
		return ""
	}
	return active.Name
}

func (r *Runtime) Restore(name string) error {
	if r == nil || r.policy == nil || name == "" {
		return nil
	}
	_, err := r.policy.Activate(name)
	return err
}

// NewRuntime 使用 Eino 官方 Skill middleware 完成 L1 列表、skill Tool 与 L2 内容注入。
func NewRuntime(
	ctx context.Context,
	packages []platformskill.Package,
	bindings []tooling.Binding,
	userInput string,
	onActivate func(platformskill.Package) error,
) (*Runtime, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	explicitName, _ := DetectExplicit(userInput, packages)
	visible := make([]platformskill.Package, 0, len(packages))
	for _, pkg := range packages {
		if !pkg.DisableModelInvocation || pkg.Name == explicitName {
			visible = append(visible, pkg)
		}
	}
	if len(visible) == 0 {
		return nil, nil
	}
	policy := newPolicy(packages, bindings)
	handler, err := einoskill.NewMiddleware(ctx, &einoskill.Config{
		Backend:    NewBackend(visible),
		UseChinese: true,
		BuildContent: func(_ context.Context, loaded einoskill.Skill, _ string) (string, error) {
			pkg, err := policy.Activate(loaded.Name)
			if err != nil {
				return "", err
			}
			if onActivate != nil {
				if err := onActivate(pkg); err != nil {
					return "", err
				}
			}
			return L2Message(pkg), nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create Eino skill middleware: %w", err)
	}
	return &Runtime{
		Handlers:     []adk.ChatModelAgentMiddleware{handler, &policyMiddleware{policy: policy}},
		ExplicitName: explicitName,
		policy:       policy,
	}, nil
}

type policy struct {
	mu       sync.RWMutex
	packages []platformskill.Package
	bindings map[string]tooling.Binding
	active   *platformskill.Package
}

func newPolicy(packages []platformskill.Package, bindings []tooling.Binding) *policy {
	byName := make(map[string]tooling.Binding, len(bindings))
	for _, binding := range bindings {
		byName[binding.Name] = binding
	}
	return &policy{packages: packages, bindings: byName}
}

func (p *policy) Activate(name string) (platformskill.Package, error) {
	pkg, ok := Find(p.packages, name)
	if !ok {
		return platformskill.Package{}, fmt.Errorf("skill %q is not pinned to this agent version", name)
	}
	for _, toolName := range pkg.AllowedTools {
		binding, exists := p.bindings[toolName]
		if !exists {
			return platformskill.Package{}, fmt.Errorf("skill requires unavailable tool %q", toolName)
		}
		if binding.RequiresNetwork && !pkg.RequiresNetwork {
			return platformskill.Package{}, fmt.Errorf("skill must declare requires_network for tool %q", toolName)
		}
	}
	copyOfPackage := pkg
	p.mu.Lock()
	p.active = &copyOfPackage
	p.mu.Unlock()
	return pkg, nil
}

func (p *policy) Active() *platformskill.Package {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.active == nil {
		return nil
	}
	copyOfPackage := *p.active
	return &copyOfPackage
}

func (p *policy) Authorize(toolName, arguments string) error {
	if toolName == "skill" {
		return nil
	}
	active := p.Active()
	if active == nil {
		return nil
	}
	if !contains(active.AllowedTools, toolName) {
		return fmt.Errorf("tool %q is not allowed by active skill %q", toolName, active.Name)
	}
	binding := p.bindings[toolName]
	if !binding.KBScoped {
		return nil
	}
	var input struct {
		KBID string `json:"kb_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil || input.KBID == "" {
		return fmt.Errorf("tool %q requires a valid kb_id", toolName)
	}
	if !contains(active.AllowedKBs, input.KBID) {
		return fmt.Errorf("knowledge base %q is not allowed by active skill %q", input.KBID, active.Name)
	}
	return nil
}

type policyMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	policy *policy
}

func (h *policyMiddleware) BeforeModelRewriteState(
	ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext,
) (context.Context, *adk.ChatModelAgentState, error) {
	active := h.policy.Active()
	if active == nil {
		return ctx, state, nil
	}
	filtered := make([]*schema.ToolInfo, 0, len(state.ToolInfos))
	for _, info := range state.ToolInfos {
		if contains(active.AllowedTools, info.Name) {
			filtered = append(filtered, info)
		}
	}
	state.ToolInfos = filtered
	return ctx, state, nil
}

func DetectExplicit(text string, packages []platformskill.Package) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", false
	}
	name := ""
	if fields[0] == "/skill" && len(fields) >= 2 {
		name = fields[1]
	} else if strings.HasPrefix(fields[0], "/") {
		name = strings.TrimPrefix(fields[0], "/")
	}
	_, ok := Find(packages, name)
	return name, ok
}

func L2Message(pkg platformskill.Package) string {
	return "[激活技能 " + pkg.Name + "]\n\n" + pkg.Instructions
}

func Find(packages []platformskill.Package, name string) (platformskill.Package, bool) {
	for _, pkg := range packages {
		if pkg.Name == name {
			return pkg, true
		}
	}
	return platformskill.Package{}, false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
