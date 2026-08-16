package llm

import "strings"

// Prompt Cache 透传规则如下。
//
// 可缓存前缀分三段、按稳定顺序排列:
//  1. system      —— Agent 系统提示(最稳定)
//  2. tools       —— 工具 JSON Schema(注册后基本不变)
//  3. skill_l1    —— L1 Skill metadata(name+description 渐进式披露第一层)
//
// 两种协议的"标记"方式不同:
//   - OpenAI 兼容(含 DeepSeek):服务端对稳定前缀**自动缓存**,无需 inline 标记;
//     客户端只要保证这三段在最前、内容稳定,即可命中;命中量从 usage.cached_tokens 读回。
//   - Anthropic:需在可缓存断点显式标 `cache_control:{type:ephemeral}`(最多 4 个断点),
//     本模块在 system / tools / skill_l1 三段各打一个断点。

// CacheSegment 是一段可缓存前缀。
type CacheSegment struct {
	Name string // system | tools | skill_l1
	Text string
}

// CacheSegments 按稳定顺序组装三段(空段自动跳过)。
func CacheSegments(system, tools, skillL1 string) []CacheSegment {
	segs := make([]CacheSegment, 0, 3)
	for _, s := range []CacheSegment{
		{Name: "system", Text: system},
		{Name: "tools", Text: tools},
		{Name: "skill_l1", Text: skillL1},
	} {
		if strings.TrimSpace(s.Text) != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// AnthropicCacheBlocks 把可缓存前缀拆成带 ephemeral cache_control 断点的 text block 列表
// (对应 Anthropic Messages API 的 system/content blocks 形态)。每段末尾一个断点。
func AnthropicCacheBlocks(segs []CacheSegment) []map[string]any {
	blocks := make([]map[string]any, 0, len(segs))
	for _, s := range segs {
		blocks = append(blocks, map[string]any{
			"type":          "text",
			"text":          s.Text,
			"cache_control": map[string]any{"type": "ephemeral"},
		})
	}
	return blocks
}

// OpenAICacheablePrefix 返回 OpenAI 兼容协议的稳定前缀文本(三段顺序拼接)。
// 服务端自动缓存,无需 inline 标记;此函数仅保证顺序稳定(命中的前提)。
func OpenAICacheablePrefix(segs []CacheSegment) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, "\n\n")
}

// CacheBreakpoints 返回会被打断点的段名(可观测 / 调试用)。
func CacheBreakpoints(segs []CacheSegment) []string {
	names := make([]string, len(segs))
	for i, s := range segs {
		names[i] = s.Name
	}
	return names
}
