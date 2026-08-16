package a2ui

import (
	"testing"
	"time"
)

func TestApprovalSurfaceIsValid(t *testing.T) {
	messages, err := ApprovalSurfaceWithPresentation(
		"ap-1", "conv-1", "refund_order", `{"order_id":"ORD-1","amount":299}`,
		ApprovalPresentation{
			OperationLabel: "订单退款",
			FieldLabels:    map[string]string{"order_id": "订单号", "amount": "退款金额"},
			FieldOrder:     []string{"order_id", "amount"},
			CurrencyFields: map[string]string{"amount": "¥"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(messages))
	}
	if !messages[0].CreateSurface.SendDataModel {
		t.Fatal("approval surface must request client data model synchronization")
	}
	if got := messages[1].UpdateComponents.Components[0].ID; got != "root" {
		t.Fatalf("want root component, got %q", got)
	}
	model := messages[2].UpdateDataModel.Value.(map[string]any)
	if got := model["arguments_summary"]; got != "订单号：ORD-1\n退款金额：¥299" {
		t.Fatalf("unexpected argument summary: %q", got)
	}
}

func TestApprovalSurfaceFallsBackToGenericPresentation(t *testing.T) {
	messages, err := ApprovalSurface("ap-2", "conv-2", "submit_action", `{"case_id":"C-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	model := messages[2].UpdateDataModel.Value.(map[string]any)
	if got := model["arguments_summary"]; got != "case id：C-1" {
		t.Fatalf("unexpected generic argument summary: %q", got)
	}
}

func TestValidateMessageRejectsUnknownComponentAndAction(t *testing.T) {
	msg := Message{Version: Version, UpdateComponents: &UpdateComponents{
		SurfaceID:  "s1",
		Components: []Component{{ID: "root", Component: "HTML", Text: "<script />"}},
	}}
	if err := ValidateMessage(msg); err == nil {
		t.Fatal("expected unknown component rejection")
	}

	msg.UpdateComponents.Components = []Component{
		{ID: "root", Component: "Button", Child: "label", Action: &Action{Event: &ActionEvent{Name: "shell.execute"}}},
		{ID: "label", Component: "Text", Text: "run"},
	}
	if err := ValidateMessage(msg); err == nil {
		t.Fatal("expected unknown action rejection")
	}
}

func TestValidateClientMessage(t *testing.T) {
	valid := ClientMessage{Version: Version, Action: &ClientAction{
		Name: ActionApprove, SurfaceID: "approval-ap-1", SourceComponentID: "approve-action",
		Timestamp: time.Now(), Context: map[string]any{"approval_id": "ap-1"},
	}}
	if err := ValidateClientMessage(valid); err != nil {
		t.Fatal(err)
	}
	valid.Action.SourceComponentID = "reject-action"
	if err := ValidateClientMessage(valid); err == nil {
		t.Fatal("expected source component rejection")
	}
}
