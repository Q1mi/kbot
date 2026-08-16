package llm

import "testing"

func TestCacheSegments_SkipsEmpty(t *testing.T) {
	segs := CacheSegments("你是助手", "", "  ")
	if len(segs) != 1 || segs[0].Name != "system" {
		t.Fatalf("expected only non-empty system segment, got %+v", segs)
	}
	full := CacheSegments("sys", "tools-schema", "skill-l1")
	if len(full) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(full))
	}
	if names := CacheBreakpoints(full); names[0] != "system" || names[1] != "tools" || names[2] != "skill_l1" {
		t.Fatalf("breakpoint order mismatch: %v", names)
	}
}

func TestAnthropicCacheBlocks_EphemeralBreakpoints(t *testing.T) {
	segs := CacheSegments("sys", "tools", "skill")
	blocks := AnthropicCacheBlocks(segs)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 cache blocks, got %d", len(blocks))
	}
	for i, b := range blocks {
		if b["type"] != "text" {
			t.Fatalf("block %d type: %v", i, b["type"])
		}
		cc, ok := b["cache_control"].(map[string]any)
		if !ok || cc["type"] != "ephemeral" {
			t.Fatalf("block %d missing ephemeral cache_control: %+v", i, b)
		}
	}
	if blocks[0]["text"] != "sys" || blocks[2]["text"] != "skill" {
		t.Fatalf("text mismatch: %+v", blocks)
	}
}

func TestOpenAICacheablePrefix_StableOrder(t *testing.T) {
	got := OpenAICacheablePrefix(CacheSegments("sys", "tools", "skill"))
	want := "sys\n\ntools\n\nskill"
	if got != want {
		t.Fatalf("prefix mismatch:\n got=%q\nwant=%q", got, want)
	}
}
