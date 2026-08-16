package a2ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ApprovalPresentation 是业务工具随 JSON Schema 声明的审批展示元数据。
// Runtime 只消费展示提示，不感知订单、保单等领域概念。
type ApprovalPresentation struct {
	Title          string
	OperationLabel string
	RiskLabel      string
	FieldLabels    map[string]string
	FieldOrder     []string
	CurrencyFields map[string]string
}

// ApprovalSurface 构造敏感工具共用的企业审批 UI。
// 未提供展示元数据时使用字段名的通用可读形式，保留旧调用方兼容性。
func ApprovalSurface(approvalID, conversationID, toolName, rawArguments string) ([]Message, error) {
	return ApprovalSurfaceWithPresentation(
		approvalID, conversationID, toolName, rawArguments, ApprovalPresentation{},
	)
}

// ApprovalSurfaceWithPresentation 使用工具版本携带的展示元数据构造审批 UI。
func ApprovalSurfaceWithPresentation(
	approvalID, conversationID, toolName, rawArguments string,
	presentation ApprovalPresentation,
) ([]Message, error) {
	var arguments any
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		arguments = rawArguments
	}
	prettyArguments, _ := json.MarshalIndent(arguments, "", "  ")
	argumentSummary := summarizeArguments(arguments, presentation)
	title := presentation.Title
	if title == "" {
		title = "敏感操作审批"
	}
	riskLabel := presentation.RiskLabel
	if riskLabel == "" {
		riskLabel = "高风险"
	}
	surfaceID := "approval-" + approvalID
	context := map[string]any{
		"approval_id":     approvalID,
		"conversation_id": conversationID,
	}
	messages := []Message{
		{
			Version: Version,
			CreateSurface: &CreateSurface{
				SurfaceID: surfaceID, CatalogID: BasicCatalog, SendDataModel: true,
			},
		},
		{
			Version: Version,
			UpdateComponents: &UpdateComponents{SurfaceID: surfaceID, Components: []Component{
				{ID: "root", Component: "Card", Child: "approval-content"},
				{ID: "approval-content", Component: "Column", Children: []string{"title-row", "status", "summary-label", "summary-value", "divider", "tool-row", "arguments-value", "resolution", "result", "actions"}},
				{ID: "title-row", Component: "Row", Children: []string{"title", "risk"}, Align: "center"},
				{ID: "title", Component: "Text", Text: title, Variant: "h5"},
				{ID: "risk", Component: "Text", Text: riskLabel, Variant: "caption"},
				{ID: "status", Component: "Text", Text: map[string]any{"path": "/status_label"}},
				{ID: "summary-label", Component: "Text", Text: "业务信息", Variant: "caption"},
				{ID: "summary-value", Component: "Text", Text: map[string]any{"path": "/arguments_summary"}},
				{ID: "divider", Component: "Divider", Axis: "horizontal"},
				{ID: "tool-row", Component: "Row", Children: []string{"tool-label", "tool-value"}, Align: "center"},
				{ID: "tool-label", Component: "Text", Text: "待执行工具", Variant: "caption"},
				{ID: "tool-value", Component: "Text", Text: map[string]any{"path": "/tool_name"}},
				{ID: "arguments-value", Component: "Text", Text: map[string]any{"path": "/arguments_display"}},
				{ID: "resolution", Component: "Text", Text: map[string]any{"path": "/resolution_label"}, Variant: "caption"},
				{ID: "result", Component: "Text", Text: map[string]any{"path": "/result_label"}},
				{ID: "actions", Component: "Row", Children: []string{"reject-action", "approve-action"}, Justify: "end", Align: "center"},
				{ID: "reject-label", Component: "Text", Text: "拒绝"},
				{ID: "reject-action", Component: "Button", Child: "reject-label", Action: &Action{Event: &ActionEvent{Name: ActionReject, Context: context}}},
				{ID: "approve-label", Component: "Text", Text: "批准并执行"},
				{ID: "approve-action", Component: "Button", Child: "approve-label", Variant: "primary", Action: &Action{Event: &ActionEvent{Name: ActionApprove, Context: context}}},
			}},
		},
		{
			Version: Version,
			UpdateDataModel: &UpdateDataModel{SurfaceID: surfaceID, Path: "/", Value: map[string]any{
				"approval_id": approvalID, "conversation_id": conversationID,
				"tool_name": toolName, "arguments": arguments, "arguments_summary": argumentSummary,
				"arguments_display": string(prettyArguments), "operation_label": operationLabel(toolName, presentation),
				"risk_level": "high", "status": "pending", "status_label": "等待人工审批",
				"resolution_label": "", "result_label": "",
			}},
		},
	}
	for i, message := range messages {
		if err := ValidateMessage(message); err != nil {
			return nil, fmt.Errorf("approval surface message %d: %w", i, err)
		}
	}
	return messages, nil
}

// ApprovalStatusMessages 增量更新客户端已有审批 surface 的状态和提示文案。
func ApprovalStatusMessages(approvalID, status string) []Message {
	label := "审批已拒绝，操作未执行"
	if status == "approved" {
		label = "审批已通过，Agent 正在执行"
	} else if status == "completed" {
		label = "操作执行完成"
	}
	return []Message{
		{Version: Version, UpdateDataModel: &UpdateDataModel{
			SurfaceID: "approval-" + approvalID, Path: "/status", Value: status,
		}},
		{Version: Version, UpdateDataModel: &UpdateDataModel{
			SurfaceID: "approval-" + approvalID, Path: "/status_label", Value: label,
		}},
	}
}

func operationLabel(toolName string, presentation ApprovalPresentation) string {
	if presentation.OperationLabel != "" {
		return presentation.OperationLabel
	}
	return strings.ReplaceAll(toolName, "_", " ")
}

func summarizeArguments(arguments any, presentation ApprovalPresentation) string {
	values, ok := arguments.(map[string]any)
	if !ok {
		return fmt.Sprint(arguments)
	}
	seen := make(map[string]bool, len(values))
	lines := make([]string, 0, len(values))
	appendValue := func(key string) {
		value, exists := values[key]
		if !exists {
			return
		}
		seen[key] = true
		label := presentation.FieldLabels[key]
		if label == "" {
			label = strings.ReplaceAll(key, "_", " ")
		}
		formatted := fmt.Sprint(value)
		if symbol, currency := presentation.CurrencyFields[key]; currency {
			formatted = symbol + strings.TrimSuffix(formatted, ".0")
		}
		lines = append(lines, fmt.Sprintf("%s：%s", label, formatted))
	}
	for _, key := range presentation.FieldOrder {
		appendValue(key)
	}
	extra := make([]string, 0, len(values))
	for key := range values {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		appendValue(key)
	}
	return strings.Join(lines, "\n")
}
