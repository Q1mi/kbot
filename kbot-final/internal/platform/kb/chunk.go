package kb

import "strings"

// chunkText 把文档切成大致等长、带少量重叠的片段。
// 优先在段落边界切，避免把一句话劈成两半。size/overlap 以"字符数"近似（中英文混排够用）。
func chunkText(text string, size, overlap int) []string {
	if size <= 0 {
		size = 500
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 5
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// 先按空行分段，再贪心地把段落拼进当前 chunk。
	paragraphs := splitParagraphs(text)

	var chunks []string
	var cur strings.Builder
	for _, p := range paragraphs {
		if cur.Len() > 0 && cur.Len()+len(p)+1 > size {
			chunks = append(chunks, cur.String())
			// 取上一个 chunk 末尾 overlap 个字符作为重叠前缀。
			tail := tailRunes(cur.String(), overlap)
			cur.Reset()
			cur.WriteString(tail)
		}
		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(p)

		// 单个超长段落：硬切。
		for cur.Len() > size {
			s := cur.String()
			cut := cutRunes(s, size)
			chunks = append(chunks, cut)
			rest := s[len(cut):]
			cur.Reset()
			cur.WriteString(tailRunes(cut, overlap))
			cur.WriteString(rest)
		}
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

func splitParagraphs(text string) []string {
	raw := strings.Split(text, "\n\n")
	var out []string
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{text}
	}
	return out
}

// tailRunes 返回 s 末尾最多 n 个 rune（按字符，不切坏多字节）。
func tailRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// cutRunes 返回 s 前 n 个 rune 对应的字节子串。
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
