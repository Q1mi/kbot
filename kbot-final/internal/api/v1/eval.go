package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Q1mi/kbot/internal/api/middleware"
	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/platform/eval"
	"github.com/Q1mi/kbot/internal/runtime/engine"
	"github.com/go-chi/chi/v5"
)

// EvalHandler 评估处理器
type EvalHandler struct {
	svc     *eval.Service
	runtime *engine.Engine
	agents  *agent.Service
}

// NewEvalHandler 创建评估处理器
func NewEvalHandler(svc *eval.Service, runtime *engine.Engine, agents ...*agent.Service) *EvalHandler {
	h := &EvalHandler{svc: svc, runtime: runtime}
	if len(agents) > 0 {
		h.agents = agents[0]
	}
	return h
}

// CreateDataset 新建评估集
func (h *EvalHandler) CreateDataset(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	var body struct {
		Name       string `json:"name"`
		TargetKind string `json:"target_kind"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	d, err := h.svc.CreateDataset(r.Context(), workspaceID, body.Name, body.TargetKind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// AddCase 加用例（支持普通用例与从对话沉淀）
func (h *EvalHandler) AddCase(w http.ResponseWriter, r *http.Request) {
	datasetID := chi.URLParam(r, "dataset_id")
	if err := h.svc.EnsureDatasetWorkspace(
		r.Context(), datasetID, middleware.GetWorkspaceIDFromContext(r.Context()),
	); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var body struct {
		Input          string `json:"input"`
		Expected       string `json:"expected"`
		ConversationID string `json:"conversation_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var c interface{}
	var err error
	if body.ConversationID != "" {
		c, err = h.svc.AddCaseFromConversation(r.Context(), datasetID, body.ConversationID, body.Input, body.Expected)
	} else {
		c, err = h.svc.AddCase(r.Context(), datasetID, body.Input, body.Expected, "")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// Run 对某 Agent 跑评估门禁（规则 Judge 或 LLM Judge）。
// @Summary  跑评估门禁(未达阈值返回 422)
// @Tags     eval
// @Security BearerAuth
// @Success  200  {object}  map[string]interface{}
// @Failure  422  {object}  map[string]interface{}  "通过率低于阈值"
// @Router   /eval/runs [post]
func (h *EvalHandler) Run(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserIDFromContext(r.Context())
	var body struct {
		DatasetID           string   `json:"dataset_id"`
		AgentID             string   `json:"agent_id"`
		AgentVersionID      string   `json:"agent_version_id"`
		JudgeMethod         string   `json:"judge_method"` // contains / exact / regex / llm-light / llm-full
		JudgeAgentID        string   `json:"judge_agent_id"`
		JudgeAgentVersionID string   `json:"judge_agent_version_id"`
		Threshold           *float64 `json:"threshold"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	threshold := 0.85
	if body.Threshold != nil {
		threshold = *body.Threshold
	}
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	if h.agents != nil {
		if err := ensureEvalAgentVersion(r.Context(), h.agents, body.AgentID, body.AgentVersionID, workspaceID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// target：用引擎跑一遍 Agent，收敛成最终文本。
	target := func(ctx context.Context, input string) (string, error) {
		return h.runtime.Chat(ctx, engine.ChatStreamRequest{
			AgentID: body.AgentID, AgentVersionID: body.AgentVersionID,
			Message: input, UserID: "eval:" + userID,
		})
	}
	var judge eval.Judge
	if body.JudgeMethod == "llm-light" || body.JudgeMethod == "llm-full" {
		if body.JudgeAgentID == "" {
			http.Error(w, "judge_agent_id is required for LLM Judge", http.StatusBadRequest)
			return
		}
		if h.agents != nil {
			if err := ensureEvalAgentVersion(
				r.Context(), h.agents, body.JudgeAgentID, body.JudgeAgentVersionID, workspaceID,
			); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		tier := "full"
		if body.JudgeMethod == "llm-light" {
			tier = "light"
		}
		judgeIdentity := body.JudgeAgentVersionID
		if judgeIdentity == "" {
			judgeIdentity = body.JudgeAgentID
		}
		judge = eval.LLMJudge{Tier: tier, AgentID: judgeIdentity, Runner: func(ctx context.Context, prompt string) (string, error) {
			return h.runtime.Chat(ctx, engine.ChatStreamRequest{
				AgentID: body.JudgeAgentID, AgentVersionID: body.JudgeAgentVersionID,
				Message: prompt, UserID: "eval-judge:" + userID,
			})
		}}
	} else {
		if body.JudgeMethod != "" && body.JudgeMethod != "contains" && body.JudgeMethod != "exact" && body.JudgeMethod != "regex" {
			http.Error(w, "unsupported judge_method", http.StatusBadRequest)
			return
		}
		judge = eval.JudgeByMethod(body.JudgeMethod)
	}

	targetID := body.AgentVersionID
	if targetID == "" {
		targetID = body.AgentID
	}
	res, err := h.svc.Run(r.Context(), eval.RunRequest{
		DatasetID:   body.DatasetID,
		TargetID:    targetID,
		WorkspaceID: workspaceID,
		Judge:       judge,
		Threshold:   threshold,
	}, target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := http.StatusOK
	if !res.Passed {
		status = http.StatusUnprocessableEntity // 422：通过率低于阈值
	}
	writeJSON(w, status, res)
}

func ensureEvalAgentVersion(
	ctx context.Context, service *agent.Service, agentID, versionID, workspaceID string,
) error {
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	versions, err := service.ListAgentVersions(ctx, agentID, workspaceID)
	if err != nil {
		return err
	}
	if versionID == "" {
		return nil
	}
	for _, version := range versions {
		if version.ID == versionID {
			return nil
		}
	}
	return fmt.Errorf("agent version does not belong to agent")
}

// ListCases 返回评估集中的全部用例。
// @Summary  列出 Eval 用例
// @Tags     eval
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /eval/datasets/{dataset_id}/cases [get]
func (h *EvalHandler) ListCases(w http.ResponseWriter, r *http.Request) {
	cases, err := h.svc.ListCases(
		r.Context(), chi.URLParam(r, "dataset_id"), middleware.GetWorkspaceIDFromContext(r.Context()),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, cases)
}

// ListRuns 返回评估运行历史和逐用例分数。
// @Summary  列出 Eval 运行历史
// @Tags     eval
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /eval/datasets/{dataset_id}/runs [get]
func (h *EvalHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.svc.ListRunHistory(
		r.Context(), chi.URLParam(r, "dataset_id"), middleware.GetWorkspaceIDFromContext(r.Context()),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// ListDatasets 列出评估集
// @Summary  列出评估集
// @Tags     eval
// @Security BearerAuth
// @Success  200  {array}  map[string]interface{}
// @Router   /eval/datasets [get]
func (h *EvalHandler) ListDatasets(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceIDFromContext(r.Context())
	ds, err := h.svc.ListDatasets(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, ds)
}
