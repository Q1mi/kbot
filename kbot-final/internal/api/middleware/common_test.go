package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseRecorderPreservesStreamingAndFirstStatus(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &responseRecorder{ResponseWriter: underlying, statusCode: http.StatusOK}

	if _, ok := any(recorder).(http.Flusher); !ok {
		t.Fatal("response recorder must preserve http.Flusher")
	}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)
	_, _ = recorder.Write([]byte("ok"))
	recorder.Flush()

	if recorder.statusCode != http.StatusCreated || underlying.Code != http.StatusCreated {
		t.Fatalf("status changed after first write: recorder=%d underlying=%d", recorder.statusCode, underlying.Code)
	}
	if !underlying.Flushed {
		t.Fatal("flush was not forwarded")
	}
}

func TestRequestIDSetsResponseHeader(t *testing.T) {
	var received string
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = w.Header().Get("X-Request-ID")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if received == "" || recorder.Header().Get("X-Request-ID") != received {
		t.Fatalf("request ID was not propagated: received=%q header=%q", received, recorder.Header().Get("X-Request-ID"))
	}
}
