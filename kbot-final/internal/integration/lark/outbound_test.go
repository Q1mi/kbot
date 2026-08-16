package lark

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestTextContent_Escapes(t *testing.T) {
	got := TextContent("he\"llo\n世界")
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("TextContent JSON 无效: %v (%s)", err, got)
	}
	if m["text"] != "he\"llo\n世界" {
		t.Fatalf("text round-trip mismatch: %q", m["text"])
	}
}

func TestSimpleCard_ValidJSON(t *testing.T) {
	got := SimpleCard("退款审批", "**金额** 100 元")
	var card map[string]any
	if err := json.Unmarshal([]byte(got), &card); err != nil {
		t.Fatalf("SimpleCard JSON 无效: %v", err)
	}
	if _, ok := card["header"]; !ok {
		t.Fatal("card 缺 header")
	}
	if _, ok := card["elements"]; !ok {
		t.Fatal("card 缺 elements")
	}
}

func TestOutbound_DisabledWhenNoCreds(t *testing.T) {
	o := NewOutbound("", "")
	if o.Enabled() {
		t.Fatal("空凭证应禁用")
	}
	if err := o.SendText(context.Background(), "open_id", "ou_x", "hi"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("禁用时应返回 ErrNotConfigured, got %v", err)
	}
	if !NewOutbound("cli_x", "secret").Enabled() {
		t.Fatal("有凭证应启用")
	}
}
