package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/Q1mi/kbot/internal/runtime/team"
)

func TestWriteTeamRunResponseAwaitingApproval(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeTeamRunResponse(recorder, "", []team.Step{{Role: "billing", AgentID: "agent-1"}},
		&engine.AwaitingApprovalError{
			ApprovalID: "approval-1", ConversationID: "conversation-1", ToolName: "freeze_refund",
		})

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	var response TeamRunResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "awaiting_approval" || response.ApprovalID != "approval-1" ||
		response.ConversationID != "conversation-1" || response.ToolName != "freeze_refund" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if len(response.Steps) != 1 || response.Steps[0].AgentID != "agent-1" {
		t.Fatalf("unexpected steps: %+v", response.Steps)
	}
}

func TestWriteTeamRunResponseCompleted(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeTeamRunResponse(recorder, "done", nil, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response TeamRunResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "completed" || response.Final != "done" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestWriteTeamRunResponseFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeTeamRunResponse(recorder, "", nil, errors.New("team failed"))

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "team failed") {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}
