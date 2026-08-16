package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUnknownAndTrailingValues(t *testing.T) {
	tests := []string{
		`{"name":"ok","unknown":true}`,
		`{"name":"ok"} {"name":"extra"}`,
	}
	for _, body := range tests {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		var value struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(httptest.NewRecorder(), req, &value); err == nil {
			t.Fatalf("decodeJSON accepted %q", body)
		}
	}
}

func TestDecodeJSONAcceptsSingleObject(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	var value struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(httptest.NewRecorder(), req, &value); err != nil {
		t.Fatal(err)
	}
	if value.Name != "ok" {
		t.Fatalf("name=%q", value.Name)
	}
}

func TestQueryIntBounds(t *testing.T) {
	tests := []struct {
		query   string
		want    int
		wantErr bool
	}{
		{"", 50, false},
		{"?limit=1", 1, false},
		{"?limit=200", 200, false},
		{"?limit=0", 0, true},
		{"?limit=-1", 0, true},
		{"?limit=201", 0, true},
		{"?limit=bad", 0, true},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/"+tt.query, nil)
		got, err := queryInt(req, "limit", 50, 1, 200)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("query %q: got (%d, %v), want (%d, error=%v)", tt.query, got, err, tt.want, tt.wantErr)
		}
	}
}
