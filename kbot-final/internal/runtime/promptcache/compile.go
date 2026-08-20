package promptcache

import (
	"encoding/json"
	"fmt"
	"sort"
	"text/template"
	"time"
	"unicode"

	einoprompt "github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

// Compile 把原始模板 + 变量 Schema 编译成 Compiled。
//
// 模板语法与 Eino schema.GoTemplate 一致。保存时用 text/template 做一次纯语法校验，
// 运行时交给 Eino ChatTemplate 完成格式化和 callback 分发。变量 Schema 解析出 required
// 键用于保存期与渲染期校验，Token 估算随版本持久化。
func Compile(versionID, raw, variablesSchemaJSON string) (*Compiled, error) {
	_, err := template.New("p").Option("missingkey=error").Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	required, err := parseRequired(variablesSchemaJSON)
	if err != nil {
		return nil, fmt.Errorf("parse variables schema: %w", err)
	}

	return &Compiled{
		VersionID:    versionID,
		Raw:          raw,
		Tmpl:         einoprompt.FromMessages(schema.GoTemplate, schema.UserMessage(raw)),
		RequiredVars: required,
		EstTokens:    EstimateTokens(raw),
		UpdatedAt:    time.Now(),
	}, nil
}

// parseRequired 从 JSON Schema 里取 required 数组(空 Schema 返回 nil)。
func parseRequired(schemaJSON string) ([]string, error) {
	if schemaJSON == "" || schemaJSON == "{}" {
		return nil, nil
	}
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &s); err != nil {
		return nil, err
	}
	sort.Strings(s.Required)
	return s.Required, nil
}

// EstimateTokens 是一个无依赖、无网络的 token 估算启发式。
//
// 设计文档 §7.9 选 pkoukk/tiktoken-go，但它会在运行时下载编码表(联网)，不利于
// 离线 build/test 复现。这里用经验启发式:CJK 字符约 1 token/字，其余按 ~4 字符/token。
// 误差对"超预算预警"这类用途够用;要精确值时换 tiktoken 只需改本函数一处。
func EstimateTokens(s string) int {
	var cjk, other int
	for _, r := range s {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + (other+3)/4
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
