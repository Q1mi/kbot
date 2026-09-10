package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	larksdk "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/Q1mi/kbot/internal/demo/lesson21"
	"github.com/Q1mi/kbot/internal/demo/logistics"
	"github.com/Q1mi/kbot/internal/integration"
)

func messageEvent(t *testing.T) *larkim.P2MessageReceiveV1 {
	t.Helper()
	var event larkim.P2MessageReceiveV1
	err := json.Unmarshal([]byte(`{"schema":"2.0","header":{"event_id":"evt-1","event_type":"im.message.receive_v1","app_id":"cli-test","tenant_key":"tenant-1"},"event":{"sender":{"sender_id":{"open_id":"ou-1"},"sender_type":"user","tenant_key":"tenant-1"},"message":{"message_id":"om-1","chat_id":"oc-1","chat_type":"p2p","message_type":"text","content":"{\"text\":\"查询 CB-1001\"}"}}}`), &event)
	if err != nil {
		t.Fatal(err)
	}
	return &event
}

type countingResponder struct {
	delegate Responder
	calls    int
}

func (r *countingResponder) Respond(ctx context.Context, message integration.Message) (integration.Reply, error) {
	r.calls++
	if r.delegate != nil {
		return r.delegate.Respond(ctx, message)
	}
	return integration.Reply{Text: "演示回复", Status: "completed"}, nil
}

type failingReplier struct {
	calls, failures int
	uuids, texts    []string
}

func (r *failingReplier) Reply(_ context.Context, _ string, text, uuid string) error {
	r.calls++
	r.uuids, r.texts = append(r.uuids, uuid), append(r.texts, text)
	if r.calls <= r.failures {
		return fmt.Errorf("temporary failure")
	}
	return nil
}

func TestFeishuEventRuntimeA2AAndSDKReply(t *testing.T) {
	ctx := context.Background()
	logisticsServer := httptest.NewUnstartedServer(nil)
	logisticsServer.Config.Handler = logistics.NewHandler("http://" + logisticsServer.Listener.Addr().String())
	logisticsServer.Start()
	defer logisticsServer.Close()
	runtime, err := lesson21.NewRuntime(ctx, logisticsServer.URL+"/.well-known/agent-card.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	dialogue, err := integration.NewDialogue(runtime.Agents, runtime.Engine, lesson21.WorkspaceID, runtime.AgentID, "dev")
	if err != nil {
		t.Fatal(err)
	}
	responder := &countingResponder{delegate: dialogue}
	tokenCalls, replyCalls := 0, 0
	var responseMu sync.Mutex
	var sent struct {
		MsgType string `json:"msg_type"`
		Content string `json:"content"`
		UUID    string `json:"uuid"`
	}
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseMu.Lock()
		defer responseMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			tokenCalls++
			var credentials map[string]string
			if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
				t.Error(err)
			}
			if credentials["app_id"] != "cli-test" || credentials["app_secret"] != "test-secret" {
				t.Error("SDK did not send configured credentials")
			}
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"test-token","expire":7200}`)
		case "/open-apis/im/v1/messages/om-1/reply":
			replyCalls++
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("unexpected authenticated request: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om-reply"}}`)
		default:
			t.Errorf("unexpected SDK path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer feishuServer.Close()
	client := larksdk.NewClient("cli-test", "test-secret", larksdk.WithOpenBaseUrl(feishuServer.URL), larksdk.WithHttpClient(feishuServer.Client()))
	bot, err := NewBot(BotConfig{AppID: "cli-test", AppSecret: "test-secret", TenantKey: "tenant-1"}, responder, NewSDKReplier(client))
	if err != nil {
		t.Fatal(err)
	}
	event := messageEvent(t)
	if err := bot.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if responder.calls != 0 {
		t.Fatal("SDK callback must enqueue before running model")
	}
	bot.deliver(ctx, <-bot.queue)
	responseMu.Lock()
	defer responseMu.Unlock()
	var content map[string]string
	if err := json.Unmarshal([]byte(sent.Content), &content); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 || replyCalls != 1 || responder.calls != 1 || sent.MsgType != "text" || sent.UUID == "" || len(sent.UUID) > 50 || !strings.Contains(content["text"], "清关中") {
		t.Fatalf("token=%d replies=%d runs=%d sent=%+v", tokenCalls, replyCalls, responder.calls, sent)
	}
	event.EventV2Base.Header.EventID = "evt-redelivery"
	if err := bot.HandleEvent(ctx, event); err != nil || len(bot.queue) != 0 {
		t.Fatalf("duplicate message queued: %v", err)
	}
}

func TestReplyRetryReusesAnswerAndUUID(t *testing.T) {
	responder := &countingResponder{}
	replier := &failingReplier{failures: 3}
	bot, err := NewBot(BotConfig{AppID: "cli-test", AppSecret: "secret"}, responder, replier)
	if err != nil {
		t.Fatal(err)
	}
	bot.retryDelay = 0
	event := messageEvent(t)
	ctx := context.Background()
	if err := bot.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	bot.deliver(ctx, <-bot.queue)
	if replier.calls != 3 {
		t.Fatalf("reply attempts=%d", replier.calls)
	}
	event.EventV2Base.Header.EventID = "evt-2"
	if err := bot.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	bot.deliver(ctx, <-bot.queue)
	if responder.calls != 1 || replier.calls != 4 {
		t.Fatalf("runs=%d replies=%d", responder.calls, replier.calls)
	}
	for index := range replier.uuids {
		if replier.uuids[index] != replier.uuids[0] || replier.texts[index] != replier.texts[0] {
			t.Fatal("retry changed reply")
		}
	}
}

func TestFeishuIdentityChecksAndQueueBackpressure(t *testing.T) {
	bot, err := NewBot(BotConfig{AppID: "cli-test", AppSecret: "secret", TenantKey: "tenant-1"}, &countingResponder{}, &failingReplier{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*larkim.P2MessageReceiveV1){
		func(e *larkim.P2MessageReceiveV1) { e.EventV2Base.Header.AppID = "other-app" },
		func(e *larkim.P2MessageReceiveV1) { e.EventV2Base.Header.TenantKey = "other-tenant" },
		func(e *larkim.P2MessageReceiveV1) { *e.Event.Sender.TenantKey = "other-tenant" },
		func(e *larkim.P2MessageReceiveV1) { e.Event.Sender.SenderId = nil },
	} {
		event := messageEvent(t)
		mutate(event)
		if err := bot.HandleEvent(context.Background(), event); err == nil {
			t.Fatal("expected identity rejection")
		}
	}
	for _, mutate := range []func(*larkim.P2MessageReceiveV1){
		func(e *larkim.P2MessageReceiveV1) { *e.Event.Sender.SenderType = "bot" },
		func(e *larkim.P2MessageReceiveV1) { *e.Event.Message.MessageType = "image" },
		func(e *larkim.P2MessageReceiveV1) { *e.Event.Message.ChatType = "group" },
	} {
		event := messageEvent(t)
		mutate(event)
		if err := bot.HandleEvent(context.Background(), event); err != nil || len(bot.queue) != 0 {
			t.Fatalf("expected ignored event: %v", err)
		}
	}
	bot.queue = make(chan *delivery, 1)
	first, second := messageEvent(t), messageEvent(t)
	*second.Event.Message.MessageId = "om-2"
	if err := bot.HandleEvent(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := bot.HandleEvent(context.Background(), second); err == nil {
		t.Fatal("expected full queue error")
	}
	<-bot.queue
	if err := bot.HandleEvent(context.Background(), second); err != nil || len(bot.queue) != 1 {
		t.Fatalf("rejected event must be retryable: %v", err)
	}
}

type notifyingReplier struct{ sent chan struct{} }

func (r notifyingReplier) Reply(context.Context, string, string, string) error {
	r.sent <- struct{}{}
	return nil
}

func TestWorkerDeduplicatesConcurrentCallbacksAndStops(t *testing.T) {
	responder := &countingResponder{}
	sent := make(chan struct{}, 32)
	bot, err := NewBot(BotConfig{AppID: "cli-test", AppSecret: "secret"}, responder, notifyingReplier{sent: sent})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); bot.work(ctx) }()
	event := messageEvent(t)
	var callbacks sync.WaitGroup
	for range 32 {
		callbacks.Go(func() {
			if err := bot.HandleEvent(ctx, event); err != nil {
				t.Error(err)
			}
		})
	}
	callbacks.Wait()
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reply")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	if responder.calls != 1 || len(sent) != 0 || len(bot.queue) != 0 {
		t.Fatalf("runs=%d extra replies=%d queued=%d", responder.calls, len(sent), len(bot.queue))
	}
}

func TestSDKReplierReportsFeishuBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"test-token-error-case","expire":7200}`)
			return
		}
		fmt.Fprint(w, `{"code":230002,"msg":"permission denied"}`)
	}))
	defer server.Close()
	client := larksdk.NewClient("cli-error-case", "test-secret", larksdk.WithOpenBaseUrl(server.URL), larksdk.WithHttpClient(server.Client()))
	err := NewSDKReplier(client).Reply(context.Background(), "om-test", "text", "uuid-test")
	if err == nil || !strings.Contains(err.Error(), "230002") {
		t.Fatalf("expected Feishu business error, got %v", err)
	}
}
