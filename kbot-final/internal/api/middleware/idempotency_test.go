package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/cache"
)

func TestIdempotencyReplaysStatusHeadersAndBodyWithinScope(t *testing.T) {
	store := cache.NewMemoryIdemStore()
	var calls atomic.Int32
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "/resources/1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))

	serve := func(userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Idempotency-Key", "request-1")
		ctx := context.WithValue(req.Context(), ContextKeyUserID, userID)
		ctx = context.WithValue(ctx, ContextKeyWorkspaceID, "workspace-1")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req.WithContext(ctx))
		return recorder
	}

	first := serve("user-1")
	replay := serve("user-1")
	otherUser := serve("user-2")

	if first.Code != http.StatusCreated || replay.Code != http.StatusCreated || otherUser.Code != http.StatusCreated {
		t.Fatalf("unexpected statuses: first=%d replay=%d other=%d", first.Code, replay.Code, otherUser.Code)
	}
	if replay.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatal("second request was not marked as a replay")
	}
	if replay.Header().Get("Location") != "/resources/1" || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay changed response: header=%q body=%q", replay.Header().Get("Location"), replay.Body.String())
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("handler calls=%d want=2", got)
	}
}

func TestIdempotencySerializesConcurrentDuplicates(t *testing.T) {
	store := cache.NewMemoryIdemStore()
	var calls atomic.Int32
	handler := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/tasks", nil)
			req.Header.Set("Idempotency-Key", "same-key")
			req = req.WithContext(context.WithValue(req.Context(), ContextKeyUserID, "user-1"))
			handler.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	close(start)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls=%d want=1", got)
	}
}

func TestIdempotencyRejectsOversizedKey(t *testing.T) {
	handler := Idempotency(cache.NewMemoryIdemStore())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	}))
	req := httptest.NewRequest(http.MethodPost, "/tasks", nil)
	req.Header.Set("Idempotency-Key", strings.Repeat("x", 257))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", recorder.Code, http.StatusBadRequest)
	}
}

func TestIdempotencyRejectsSameKeyWithDifferentBody(t *testing.T) {
	handler := Idempotency(cache.NewMemoryIdemStore())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	serve := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "stable-key")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	if first := serve(`{"name":"one"}`); first.Code != http.StatusCreated {
		t.Fatalf("first status=%d", first.Code)
	}
	if second := serve(`{"name":"two"}`); second.Code != http.StatusConflict {
		t.Fatalf("second status=%d want=%d", second.Code, http.StatusConflict)
	}
}
