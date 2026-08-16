package prompt

import "strings"

// UnifiedDiff 产生两段文本的行级 unified diff（无第三方依赖的 LCS 实现）。
// 共同行前缀空格，仅旧有的行前缀 "-"，仅新有的行前缀 "+"。
func UnifiedDiff(oldText, newText string) string {
	a := strings.Split(oldText, "\n")
	b := strings.Split(newText, "\n")

	// LCS 长度表。
	m, n := len(a), len(b)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var sb strings.Builder
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			sb.WriteString("  " + a[i] + "\n")
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			sb.WriteString("- " + a[i] + "\n")
			i++
		default:
			sb.WriteString("+ " + b[j] + "\n")
			j++
		}
	}
	for ; i < m; i++ {
		sb.WriteString("- " + a[i] + "\n")
	}
	for ; j < n; j++ {
		sb.WriteString("+ " + b[j] + "\n")
	}
	return sb.String()
}
