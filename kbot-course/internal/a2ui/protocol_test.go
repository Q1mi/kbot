package a2ui

import "testing"

func TestApprovalMessagesUseConstrainedV09Catalog(t *testing.T) {
	messages := ApprovalMessages("approval-1", "conversation-1", "create_transfer", map[string]any{"quantity": 2})
	if len(messages) != 3 {
		t.Fatalf("message count = %d", len(messages))
	}
	for _, message := range messages {
		if err := Validate(message); err != nil {
			t.Fatalf("validate %#v: %v", message, err)
		}
	}
	components := messages[1].UpdateComponents.Components
	if components[len(components)-1].Action.Event.Name != "approval.approve" {
		t.Fatalf("approve action = %#v", components[len(components)-1].Action)
	}
}

func TestValidateRejectsUnknownActionAndMultipleEnvelopes(t *testing.T) {
	message := Message{Version: Version, UpdateComponents: &UpdateComponents{SurfaceID: "s", Components: []Component{{
		ID: "button", Component: "Button", Action: &ActionDef{Event: &ActionEvent{Name: "shell.exec"}},
	}}}}
	if err := Validate(message); err == nil {
		t.Fatal("unknown action was accepted")
	}
	message = Message{Version: Version, CreateSurface: &CreateSurface{SurfaceID: "s", CatalogID: BasicCatalog}, DeleteSurface: &DeleteSurface{SurfaceID: "s"}}
	if err := Validate(message); err == nil {
		t.Fatal("multiple envelopes were accepted")
	}
}
