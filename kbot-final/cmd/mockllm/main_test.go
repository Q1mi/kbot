package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatCompletionsRequestsRefundTool(t *testing.T) {
	body := []byte(`{"model":"demo","messages":[{"role":"user","content":"请把订单 KBOT-2026-004 退款 199 元，原因是重复购买"}],"tools":[{"type":"function","function":{"name":"refund_order"}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				ToolCalls []struct {
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Choices) != 1 || got.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls, got %+v", got.Choices)
	}
	if len(got.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", got.Choices[0].Message.ToolCalls)
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(got.Choices[0].Message.ToolCalls[0].Function.Arguments), &arguments); err != nil {
		t.Fatal(err)
	}
	if arguments["order_id"] != "KBOT-2026-004" || arguments["amount"] != float64(199) || arguments["reason"] != "重复购买" {
		t.Fatalf("tool arguments do not follow the user request: %+v", arguments)
	}
}

func TestRefundToolReturnsDeterministicID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tools/refund", bytes.NewBufferString(`{"order_id":"KB20260725","amount":299}`))
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("RF-20260725-001")) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
