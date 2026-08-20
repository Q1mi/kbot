// Package promptcache 是 Apollo 风格 Prompt 中心的客户端缓存(设计文档 §4.2 / 讲义 §14.4)。
//
// Runtime 不每次对话都查 DB——把模板、变量约束、token 估算编译成"编译产物"
// (Compiled),放进本地 sync.Map，命中靠内存、毫秒级。Platform 改 env 指针时通过
// Redis Pub/Sub 广播失效，Runtime 异步重拉(见 subscriber.go)。
package promptcache

import (
	"context"
	"fmt"
	"sync"
	"time"

	einoprompt "github.com/cloudwego/eino/components/prompt"
)

// Compiled 是一个 Prompt 版本的编译产物。
type Compiled struct {
	VersionID    string
	Raw          string
	Tmpl         einoprompt.ChatTemplate
	RequiredVars []string // 变量 Schema 里 required 的键(保存时解析)
	EstTokens    int
	UpdatedAt    time.Time
}

// Render 用给定变量渲染模板。missingkey=error 保证缺变量立刻报错而非渲染成 <no value>。
func (c *Compiled) Render(ctx context.Context, vars map[string]any) (string, error) {
	// 先做 required 校验，给出比 template 更清晰的错误。
	for _, k := range c.RequiredVars {
		if _, ok := vars[k]; !ok {
			return "", fmt.Errorf("prompt 缺少必填变量 %q", k)
		}
	}
	if c.Tmpl == nil {
		return c.Raw, nil
	}
	messages, err := c.Tmpl.Format(ctx, vars)
	if err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}
	if len(messages) != 1 {
		return "", fmt.Errorf("render prompt: expected one message, got %d", len(messages))
	}
	return messages[0].Content, nil
}

// Cache 是 "promptID@env" → *Compiled 的本地缓存。
type Cache struct {
	mp sync.Map
}

// NewCache 创建缓存。
func NewCache() *Cache { return &Cache{} }

func key(promptID, env string) string { return promptID + "@" + env }

// Get 取某 prompt 在某 env 当前指向版本的编译产物。
func (c *Cache) Get(promptID, env string) (*Compiled, bool) {
	v, ok := c.mp.Load(key(promptID, env))
	if !ok {
		return nil, false
	}
	return v.(*Compiled), true
}

// Put 写入编译产物。
func (c *Cache) Put(promptID, env string, comp *Compiled) {
	c.mp.Store(key(promptID, env), comp)
}

// Invalidate 失效某 prompt@env 的缓存。
func (c *Cache) Invalidate(promptID, env string) {
	c.mp.Delete(key(promptID, env))
}
