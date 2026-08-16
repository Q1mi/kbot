package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCompletions(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"demo","messages":[{"role":"user","content":"你能做什么"}]
	}`))
	recorder := httptest.NewRecorder()
	newMux().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "回答知识问题") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}
