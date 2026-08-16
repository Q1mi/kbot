package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadiness(t *testing.T) {
	h := &Handler{}
	h.SetReadiness(ReadinessCheck{Name: "postgres", Check: func(context.Context) error { return nil }})
	rec := httptest.NewRecorder()
	h.ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d want=%d", rec.Code, http.StatusOK)
	}
}

func TestReadiness_DependencyFailure(t *testing.T) {
	h := &Handler{}
	h.SetReadiness(ReadinessCheck{Name: "redis", Check: func(context.Context) error { return errors.New("down") }})
	rec := httptest.NewRecorder()
	h.ready(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d want=%d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Body.String(); got != "not ready: redis\n" {
		t.Fatalf("body=%q", got)
	}
}
