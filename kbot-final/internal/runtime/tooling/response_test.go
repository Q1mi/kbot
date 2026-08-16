package tooling

import (
	"strings"
	"testing"
)

func TestReadToolResponseRejectsOversizedBody(t *testing.T) {
	_, err := readToolResponse(strings.NewReader(strings.Repeat("x", int(maxToolResponseBytes)+1)))
	if err == nil {
		t.Fatal("oversized tool response was accepted")
	}
}
