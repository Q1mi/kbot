// Package a2ui implements the constrained A2UI v0.9 subset used by kbot approvals.
package a2ui

import (
	"fmt"
	"strings"
)

const (
	Version      = "v0.9"
	BasicCatalog = "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json"
)

type Message struct {
	Version          string            `json:"version"`
	CreateSurface    *CreateSurface    `json:"createSurface,omitempty"`
	UpdateComponents *UpdateComponents `json:"updateComponents,omitempty"`
	UpdateDataModel  *UpdateDataModel  `json:"updateDataModel,omitempty"`
	DeleteSurface    *DeleteSurface    `json:"deleteSurface,omitempty"`
}

type CreateSurface struct {
	SurfaceID     string `json:"surfaceId"`
	CatalogID     string `json:"catalogId"`
	SendDataModel bool   `json:"sendDataModel,omitempty"`
}

type UpdateComponents struct {
	SurfaceID  string      `json:"surfaceId"`
	Components []Component `json:"components"`
}

type UpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path,omitempty"`
	Value     any    `json:"value"`
}

type DeleteSurface struct {
	SurfaceID string `json:"surfaceId"`
}

type Component struct {
	ID        string     `json:"id"`
	Component string     `json:"component"`
	Text      any        `json:"text,omitempty"`
	Variant   string     `json:"variant,omitempty"`
	Children  []string   `json:"children,omitempty"`
	Child     string     `json:"child,omitempty"`
	Action    *ActionDef `json:"action,omitempty"`
}

type ActionDef struct {
	Event *ActionEvent `json:"event,omitempty"`
}
type ActionEvent struct {
	Name    string         `json:"name"`
	Context map[string]any `json:"context,omitempty"`
}

type ActionRequest struct {
	Version string `json:"version"`
	Action  struct {
		Name              string         `json:"name"`
		SurfaceID         string         `json:"surfaceId"`
		SourceComponentID string         `json:"sourceComponentId"`
		Context           map[string]any `json:"context"`
	} `json:"action"`
}

var allowedComponents = map[string]struct{}{"Text": {}, "Card": {}, "Column": {}, "Row": {}, "Button": {}, "Divider": {}}
var allowedActions = map[string]struct{}{"approval.approve": {}, "approval.reject": {}}

func Validate(message Message) error {
	if message.Version != Version {
		return fmt.Errorf("unsupported A2UI version %q", message.Version)
	}
	envelopes := 0
	for _, present := range []bool{message.CreateSurface != nil, message.UpdateComponents != nil, message.UpdateDataModel != nil, message.DeleteSurface != nil} {
		if present {
			envelopes++
		}
	}
	if envelopes != 1 {
		return fmt.Errorf("A2UI message must contain exactly one envelope")
	}
	if create := message.CreateSurface; create != nil {
		if strings.TrimSpace(create.SurfaceID) == "" || create.CatalogID != BasicCatalog {
			return fmt.Errorf("A2UI surface id or catalog is not allowed")
		}
	}
	if update := message.UpdateComponents; update != nil {
		if update.SurfaceID == "" || len(update.Components) == 0 || len(update.Components) > 64 {
			return fmt.Errorf("A2UI component update exceeds limits")
		}
		seen := make(map[string]struct{}, len(update.Components))
		for _, component := range update.Components {
			if component.ID == "" {
				return fmt.Errorf("A2UI component id is required")
			}
			if _, duplicate := seen[component.ID]; duplicate {
				return fmt.Errorf("duplicate A2UI component id %q", component.ID)
			}
			seen[component.ID] = struct{}{}
			if _, ok := allowedComponents[component.Component]; !ok {
				return fmt.Errorf("A2UI component %q is outside the catalog", component.Component)
			}
			if component.Action != nil && component.Action.Event != nil {
				if _, ok := allowedActions[component.Action.Event.Name]; !ok {
					return fmt.Errorf("A2UI action %q is not allowed", component.Action.Event.Name)
				}
			}
		}
	}
	if update := message.UpdateDataModel; update != nil {
		if update.SurfaceID == "" || (update.Path != "" && !strings.HasPrefix(update.Path, "/")) {
			return fmt.Errorf("A2UI data model update is invalid")
		}
	}
	return nil
}

func ApprovalMessages(approvalID, conversationID, toolName string, arguments map[string]any) []Message {
	surfaceID := "approval-" + approvalID
	context := func() map[string]any {
		return map[string]any{"approval_id": approvalID, "conversation_id": conversationID, "surface_id": surfaceID}
	}
	return []Message{
		{Version: Version, CreateSurface: &CreateSurface{SurfaceID: surfaceID, CatalogID: BasicCatalog, SendDataModel: true}},
		{Version: Version, UpdateComponents: &UpdateComponents{SurfaceID: surfaceID, Components: []Component{
			{ID: "approval-card", Component: "Card", Child: "approval-column"},
			{ID: "approval-column", Component: "Column", Children: []string{"approval-title", "approval-summary", "approval-actions"}},
			{ID: "approval-title", Component: "Text", Text: "需要人工审批：" + toolName, Variant: "heading"},
			{ID: "approval-summary", Component: "Text", Text: map[string]any{"path": "/summary"}},
			{ID: "approval-actions", Component: "Row", Children: []string{"approval-reject", "approval-approve"}},
			{ID: "approval-reject", Component: "Button", Text: "拒绝", Variant: "secondary", Action: &ActionDef{Event: &ActionEvent{Name: "approval.reject", Context: context()}}},
			{ID: "approval-approve", Component: "Button", Text: "批准", Variant: "primary", Action: &ActionDef{Event: &ActionEvent{Name: "approval.approve", Context: context()}}},
		}}},
		{Version: Version, UpdateDataModel: &UpdateDataModel{SurfaceID: surfaceID, Path: "/", Value: map[string]any{
			"approval_id": approvalID, "conversation_id": conversationID, "tool_name": toolName,
			"arguments": arguments, "summary": fmt.Sprintf("工具 %s 将使用参数 %v", toolName, arguments), "status": "pending",
		}}},
	}
}
