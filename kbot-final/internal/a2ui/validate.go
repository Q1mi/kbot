package a2ui

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxComponents    = 64
	maxDataModelSize = 64 << 10
	maxContextSize   = 16 << 10
)

var allowedComponents = map[string]bool{
	"Text": true, "Card": true, "Column": true, "Row": true, "Button": true, "Divider": true,
}

var allowedActions = map[string]bool{ActionApprove: true, ActionReject: true}

// ValidateMessage 在消息进入传输层前执行 catalog、规模、引用和 action 白名单校验。
func ValidateMessage(msg Message) error {
	if msg.Version != Version {
		return fmt.Errorf("unsupported A2UI version %q", msg.Version)
	}
	count := 0
	if msg.CreateSurface != nil {
		count++
		if err := validateSurfaceID(msg.CreateSurface.SurfaceID); err != nil {
			return err
		}
		if msg.CreateSurface.CatalogID != BasicCatalog {
			return fmt.Errorf("catalog %q is not allowed", msg.CreateSurface.CatalogID)
		}
	}
	if msg.UpdateComponents != nil {
		count++
		if err := validateComponents(*msg.UpdateComponents); err != nil {
			return err
		}
	}
	if msg.UpdateDataModel != nil {
		count++
		if err := validateSurfaceID(msg.UpdateDataModel.SurfaceID); err != nil {
			return err
		}
		if msg.UpdateDataModel.Path != "" && !strings.HasPrefix(msg.UpdateDataModel.Path, "/") {
			return fmt.Errorf("data model path must be a JSON Pointer")
		}
		encoded, err := json.Marshal(msg.UpdateDataModel.Value)
		if err != nil {
			return fmt.Errorf("marshal data model: %w", err)
		}
		if len(encoded) > maxDataModelSize {
			return fmt.Errorf("data model exceeds %d bytes", maxDataModelSize)
		}
	}
	if msg.DeleteSurface != nil {
		count++
		if err := validateSurfaceID(msg.DeleteSurface.SurfaceID); err != nil {
			return err
		}
	}
	if count != 1 {
		return fmt.Errorf("A2UI message must contain exactly one envelope")
	}
	return nil
}

func validateComponents(update UpdateComponents) error {
	if err := validateSurfaceID(update.SurfaceID); err != nil {
		return err
	}
	if len(update.Components) == 0 || len(update.Components) > maxComponents {
		return fmt.Errorf("component count must be between 1 and %d", maxComponents)
	}
	ids := make(map[string]bool, len(update.Components))
	for _, component := range update.Components {
		if component.ID == "" || len(component.ID) > 128 {
			return fmt.Errorf("invalid component id %q", component.ID)
		}
		if ids[component.ID] {
			return fmt.Errorf("duplicate component id %q", component.ID)
		}
		ids[component.ID] = true
		if !allowedComponents[component.Component] {
			return fmt.Errorf("component %q is not in the kbot catalog", component.Component)
		}
		if component.Action != nil {
			if component.Component != "Button" || component.Action.Event == nil {
				return fmt.Errorf("component %q has an invalid action", component.ID)
			}
			if !allowedActions[component.Action.Event.Name] {
				return fmt.Errorf("action %q is not allowed", component.Action.Event.Name)
			}
			encoded, _ := json.Marshal(component.Action.Event.Context)
			if len(encoded) > maxContextSize {
				return fmt.Errorf("action context exceeds %d bytes", maxContextSize)
			}
		}
	}
	if !ids["root"] {
		return fmt.Errorf("component tree is missing root")
	}
	for _, component := range update.Components {
		refs := append([]string{}, component.Children...)
		if component.Child != "" {
			refs = append(refs, component.Child)
		}
		for _, ref := range refs {
			if !ids[ref] {
				return fmt.Errorf("component %q references unknown child %q", component.ID, ref)
			}
		}
	}
	return nil
}

// ValidateClientMessage 校验浏览器提交的 action envelope 与受控动作来源。
func ValidateClientMessage(msg ClientMessage) error {
	if msg.Version != Version {
		return fmt.Errorf("unsupported A2UI version %q", msg.Version)
	}
	if (msg.Action == nil) == (msg.Error == nil) {
		return fmt.Errorf("A2UI client message must contain exactly one action or error")
	}
	if msg.Error != nil {
		if err := validateSurfaceID(msg.Error.SurfaceID); err != nil {
			return err
		}
		if msg.Error.Code == "" || msg.Error.Message == "" {
			return fmt.Errorf("error code and message are required")
		}
		return nil
	}
	action := msg.Action
	if err := validateSurfaceID(action.SurfaceID); err != nil {
		return err
	}
	if !allowedActions[action.Name] {
		return fmt.Errorf("action %q is not allowed", action.Name)
	}
	expectedSource := "approve-action"
	if action.Name == ActionReject {
		expectedSource = "reject-action"
	}
	if action.SourceComponentID != expectedSource {
		return fmt.Errorf("action %q cannot originate from %q", action.Name, action.SourceComponentID)
	}
	if action.Timestamp.IsZero() {
		return fmt.Errorf("action timestamp is required")
	}
	encoded, err := json.Marshal(action.Context)
	if err != nil || len(encoded) > maxContextSize {
		return fmt.Errorf("invalid action context")
	}
	return nil
}

func validateSurfaceID(id string) error {
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("invalid surface id %q", id)
	}
	return nil
}
