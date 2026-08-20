package retriever

import (
	"context"
	"testing"
)

type stubSearcher struct {
	results   []Result
	workspace string
}

func (s *stubSearcher) Search(_ context.Context, workspaceID, _ string, _ int) ([]Result, error) {
	s.workspace = workspaceID
	return append([]Result(nil), s.results...), nil
}

func TestHybridUsesReciprocalRankFusion(t *testing.T) {
	keyword := &stubSearcher{results: []Result{{ID: "keyword-only"}, {ID: "shared", SourceURI: "policy.md", Text: "七天退款"}}}
	vector := &stubSearcher{results: []Result{{ID: "vector-only"}, {ID: "shared", SourceURI: "policy.md", Text: "七天退款"}}}
	hybrid := NewHybrid(keyword, vector)
	results, err := hybrid.Search(context.Background(), "ws-1", "退款政策", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 || results[0].ID != "shared" {
		t.Fatalf("results = %+v", results)
	}
	if keyword.workspace != "ws-1" || vector.workspace != "ws-1" {
		t.Fatal("workspace filter was not propagated")
	}
}

func TestHybridValidatesQuery(t *testing.T) {
	_, err := NewHybrid(&stubSearcher{}, &stubSearcher{}).Search(context.Background(), "ws", "", 3)
	if err == nil {
		t.Fatal("expected empty query to fail")
	}
}

func TestHybridDoesNotMixRawScoresIntoRRF(t *testing.T) {
	keyword := &stubSearcher{results: []Result{{ID: "keyword-only", Score: 1e12}, {ID: "shared", Score: 0.01}}}
	vector := &stubSearcher{results: []Result{{ID: "shared", Score: -200}, {ID: "vector-only", Score: 999}}}
	results, err := NewHybrid(keyword, vector).Search(context.Background(), "ws", "query", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].ID != "shared" {
		t.Fatalf("RRF results = %#v; shared result should win by appearing in both rankings", results)
	}
	if results[0].Score >= 1 {
		t.Fatalf("RRF score still contains a raw retrieval score: %f", results[0].Score)
	}
}
