package lark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	larksdk "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/Q1mi/kbot/internal/integration"
)

type BotConfig struct{ AppID, AppSecret, TenantKey string }

type Responder interface {
	Respond(context.Context, integration.Message) (integration.Reply, error)
}

type Replier interface {
	Reply(context.Context, string, string, string) error // messageID, text, uuid
}

type delivery struct {
	message      integration.Message
	text         string // 生成一次，发送重试复用同一份回答。
	queued, done bool
	expires      time.Time
}

// Bot 使用官方 SDK 长连接收事件，通过后台队列调用 Runtime 并回复原消息。
// 队列、去重和待发送回答保存在内存，适用于本课单实例演示。
type Bot struct {
	config     BotConfig
	responder  Responder
	replier    Replier
	queue      chan *delivery
	mu         sync.Mutex
	deliveries map[string]*delivery
	retryDelay time.Duration
}

func NewBot(config BotConfig, responder Responder, replier Replier) (*Bot, error) {
	if strings.TrimSpace(config.AppID) == "" || strings.TrimSpace(config.AppSecret) == "" || responder == nil {
		return nil, fmt.Errorf("Feishu App ID, App Secret and Runtime responder are required")
	}
	if replier == nil {
		replier = NewSDKReplier(larksdk.NewClient(config.AppID, config.AppSecret,
			larksdk.WithHttpClient(&http.Client{Timeout: 15 * time.Second}), larksdk.WithLogLevel(larkcore.LogLevelWarn)))
	}
	return &Bot{config: config, responder: responder, replier: replier, queue: make(chan *delivery, 128), deliveries: make(map[string]*delivery), retryDelay: time.Second}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); b.work(ctx) }()
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(b.HandleEvent)
	client := larkws.NewClient(b.config.AppID, b.config.AppSecret,
		larkws.WithEventHandler(handler), larkws.WithLogLevel(larkcore.LogLevelWarn),
		larkws.WithOnReady(func() { log.Print("Feishu long connection ready; send a text message to the bot") }))
	err := client.Start(ctx)
	cancel()
	<-workerDone
	return err
}

// HandleEvent 只验证、规范化和入队，避免等待模型阻塞 SDK 的事件确认。
func (b *Bot) HandleEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	message, accepted, err := b.decode(event)
	if err != nil || !accepted {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := deliveryKey(message)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for id, item := range b.deliveries {
		if !item.queued && now.After(item.expires) {
			delete(b.deliveries, id)
		}
	}
	item, exists := b.deliveries[key]
	if exists && (item.queued || item.done) {
		return nil
	}
	if !exists {
		if len(b.deliveries) >= 4096 {
			return fmt.Errorf("Feishu deduplication capacity reached")
		}
		item = &delivery{message: message, expires: now.Add(time.Hour)}
	}
	select {
	case b.queue <- item:
		item.queued = true
		b.deliveries[key] = item
		return nil
	default:
		return fmt.Errorf("Feishu incoming queue is full")
	}
}

func (b *Bot) decode(event *larkim.P2MessageReceiveV1) (integration.Message, bool, error) {
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Message == nil {
		return integration.Message{}, false, fmt.Errorf("incomplete Feishu event")
	}
	header, sender, message := event.EventV2Base.Header, event.Event.Sender, event.Event.Message
	if header.AppID != b.config.AppID || header.TenantKey == "" || (b.config.TenantKey != "" && header.TenantKey != b.config.TenantKey) {
		return integration.Message{}, false, fmt.Errorf("Feishu application or tenant mismatch")
	}
	if sender.TenantKey != nil && *sender.TenantKey != header.TenantKey {
		return integration.Message{}, false, fmt.Errorf("Feishu sender tenant mismatch")
	}
	if value(sender.SenderType) != "user" || value(message.MessageType) != "text" {
		return integration.Message{}, false, nil
	}
	if value(message.ChatType) != "p2p" && value(message.ChatType) != "group" {
		return integration.Message{}, false, nil
	}
	// 群聊仅订阅 group_at_msg 事件，避免读取全量群消息。无 mention 的群消息直接忽略。
	if value(message.ChatType) == "group" && len(message.Mentions) == 0 {
		return integration.Message{}, false, nil
	}
	if sender.SenderId == nil || value(sender.SenderId.OpenId) == "" || value(message.MessageId) == "" || value(message.ChatId) == "" || header.EventID == "" {
		return integration.Message{}, false, fmt.Errorf("Feishu event, message, chat and sender ids are required")
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(value(message.Content)), &content); err != nil {
		return integration.Message{}, false, fmt.Errorf("invalid Feishu text content")
	}
	for _, mention := range message.Mentions {
		if mention == nil || value(mention.Key) == "" {
			continue
		}
		replacement := "@" + value(mention.Name)
		if value(mention.MentionedType) == "bot" {
			replacement = ""
		}
		content.Text = strings.ReplaceAll(content.Text, value(mention.Key), replacement)
	}
	content.Text = strings.TrimSpace(content.Text)
	if content.Text == "" {
		return integration.Message{}, false, nil
	}
	return integration.Message{Source: "lark", AppID: header.AppID, TenantKey: header.TenantKey,
		EventID: header.EventID, MessageID: *message.MessageId, ChatID: *message.ChatId,
		UserID: *sender.SenderId.OpenId, Text: content.Text}, true, nil
}

func (b *Bot) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-b.queue:
			if ctx.Err() != nil {
				return
			}
			b.deliver(ctx, item)
		}
	}
}

func (b *Bot) deliver(ctx context.Context, item *delivery) {
	if item.text == "" {
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		reply, err := b.responder.Respond(runCtx, item.message)
		cancel()
		item.text = reply.Text
		if err != nil {
			log.Printf("Feishu Runtime failed message_id=%s: %v", item.message.MessageID, err)
			item.text = "本次处理失败，请稍后发送新消息重试。"
		}
		if strings.TrimSpace(item.text) == "" {
			item.text = "本次未生成文本回复，请稍后发送新消息重试。"
		}
		// 控制单条回复长度；保留 Unicode 字符完整性。
		if chars := []rune(item.text); len(chars) > 12000 {
			item.text = string(chars[:12000]) + "\n（回答过长，已截断。）"
		}
		log.Printf("Feishu Runtime finished message_id=%s conversation_id=%s agent_version_id=%s status=%s", item.message.MessageID, reply.ConversationID, reply.AgentVersionID, reply.Status)
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = b.replier.Reply(ctx, item.message.MessageID, item.text, deliveryKey(item.message))
		if err == nil || ctx.Err() != nil {
			break
		}
		if attempt < 2 {
			timer := time.NewTimer(b.retryDelay * time.Duration(attempt+1))
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
			if ctx.Err() != nil {
				break
			}
		}
	}
	b.mu.Lock()
	item.queued, item.done = false, err == nil
	// 与飞书 uuid 的一小时去重窗口一致。失败记录保留回答，重复投递只重试发送。
	item.expires = time.Now().Add(time.Hour)
	b.mu.Unlock()
	if err != nil {
		log.Printf("Feishu reply failed message_id=%s: %v", item.message.MessageID, err)
	}
}

func value(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

func deliveryKey(message integration.Message) string {
	identity, _ := json.Marshal([]string{message.AppID, message.TenantKey, message.MessageID})
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:20]) // 40 字符，满足回复 uuid 长度限制。
}

type SDKReplier struct{ client *larksdk.Client }

func NewSDKReplier(client *larksdk.Client) *SDKReplier { return &SDKReplier{client: client} }

func (r *SDKReplier) Reply(ctx context.Context, messageID, text, uuid string) error {
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	response, err := r.client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType("text").Content(string(content)).Uuid(uuid).Build()).Build())
	if err != nil {
		return fmt.Errorf("Feishu reply request: %w", err)
	}
	if response == nil {
		return fmt.Errorf("Feishu reply returned no response")
	}
	if !response.Success() {
		return fmt.Errorf("Feishu reply API code=%d", response.Code)
	}
	return nil
}
