package lark

// 飞书出站使用 larksuite/oapi-sdk-go v3 发文本 + 交互卡片。
// AppID/Secret 为空则禁用(Send 返回 ErrNotConfigured),seed/启动不强制飞书。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// ErrNotConfigured 表示未配置飞书出站凭证。
var ErrNotConfigured = errors.New("lark outbound not configured (set KBOT_LARK_APP_ID/SECRET)")

// Outbound 飞书出站客户端。
type Outbound struct {
	client *lark.Client
}

// NewOutbound 创建出站客户端;appID/secret 任一为空则禁用(client 为 nil)。
func NewOutbound(appID, appSecret string) *Outbound {
	if appID == "" || appSecret == "" {
		return &Outbound{}
	}
	return &Outbound{client: lark.NewClient(appID, appSecret)}
}

// Enabled 报告出站是否可用(已配置凭证)。
func (o *Outbound) Enabled() bool { return o.client != nil }

// SendText 发纯文本消息。receiveIDType 取 open_id / user_id / union_id / email / chat_id。
func (o *Outbound) SendText(ctx context.Context, receiveIDType, receiveID, text string) error {
	return o.send(ctx, receiveIDType, receiveID, "text", TextContent(text), "")
}

// SendTextIdempotent 使用飞书消息 UUID 去重，同一 UUID 一小时内至多成功发送一次。
func (o *Outbound) SendTextIdempotent(ctx context.Context, receiveIDType, receiveID, text, idempotencyKey string) error {
	return o.send(ctx, receiveIDType, receiveID, "text", TextContent(text), idempotencyKey)
}

// SendCard 发交互卡片(cardJSON 为飞书卡片 JSON)。
func (o *Outbound) SendCard(ctx context.Context, receiveIDType, receiveID, cardJSON string) error {
	return o.send(ctx, receiveIDType, receiveID, "interactive", cardJSON, "")
}

func (o *Outbound) send(ctx context.Context, receiveIDType, receiveID, msgType, content, idempotencyKey string) error {
	if o.client == nil {
		return ErrNotConfigured
	}
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(receiveID).
		MsgType(msgType).
		Content(content)
	if idempotencyKey != "" {
		body.Uuid(idempotencyKey)
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(body.Build()).
		Build()
	resp, err := o.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("lark send: %w", err)
	}
	if resp.Code != 0 { // 飞书约定 code==0 为成功
		return fmt.Errorf("lark send failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// TextContent 把纯文本包成飞书 text 消息 content:{"text":"..."}(正确 JSON 转义)。
func TextContent(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}

// SimpleCard 生成最简交互卡片(标题 + 一段 lark_md 正文)的 content JSON。
func SimpleCard(title, body string) string {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": title},
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": body}},
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}
