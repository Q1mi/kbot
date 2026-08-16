package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Q1mi/kbot/internal/platform/agent"
	"github.com/Q1mi/kbot/internal/runtime/engine"
)

type ChatRuntime interface {
	ChatStream(context.Context, engine.ChatRequest, engine.Emitter) error
}

type RuntimeConsumer struct {
	agents      *agent.Service
	runtime     ChatRuntime
	workspaceID string
	agentID     string
	environment string
	logAnswer   func(source, eventID, answer string)
}

func NewRuntimeConsumer(agents *agent.Service, runtime ChatRuntime, workspaceID, agentID, environment string, logAnswer func(string, string, string)) *RuntimeConsumer {
	if environment == "" {
		environment = "prod"
	}
	return &RuntimeConsumer{agents: agents, runtime: runtime, workspaceID: workspaceID, agentID: agentID, environment: environment, logAnswer: logAnswer}
}

func (c *RuntimeConsumer) Callback(source string) func([]byte) error {
	return func(body []byte) error { return c.Consume(context.Background(), source, body) }
}

func (c *RuntimeConsumer) Consume(ctx context.Context, source string, body []byte) error {
	if c == nil || c.agents == nil || c.runtime == nil || c.workspaceID == "" || c.agentID == "" {
		return fmt.Errorf("channel runtime and fixed workspace/agent mapping are required")
	}
	message, err := decodeMessage(source, body)
	if err != nil {
		return err
	}
	conversation, err := c.agents.CreateConversation(ctx, c.workspaceID, c.agentID, c.environment, "channel:"+source+":"+message.UserID)
	if err != nil {
		return err
	}
	answer := ""
	err = c.runtime.ChatStream(ctx, engine.ChatRequest{
		ConversationID: conversation.ID, WorkspaceID: c.workspaceID,
		UserID: "channel:" + source + ":" + message.UserID, Message: message.Text,
	}, func(event engine.Event) error {
		if event.Type == "answer_done" {
			answer = event.Text
		}
		return nil
	})
	if err != nil {
		return err
	}
	if c.logAnswer != nil {
		c.logAnswer(source, message.EventID, answer)
	}
	return nil
}

func decodeMessage(source string, body []byte) (Message, error) {
	var direct struct {
		EventID string `json:"event_id"`
		UserID  string `json:"user_id"`
		Text    string `json:"text"`
		Header  struct {
			EventID string `json:"event_id"`
		} `json:"header"`
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &direct); err != nil {
		return Message{}, fmt.Errorf("decode %s event: %w", source, err)
	}
	message := Message{Source: source, EventID: direct.EventID, UserID: direct.UserID, Text: direct.Text}
	if source == "lark" {
		message.EventID, message.UserID = direct.Header.EventID, direct.Event.Sender.SenderID.OpenID
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(direct.Event.Message.Content), &content); err != nil {
			return Message{}, fmt.Errorf("decode lark message content: %w", err)
		}
		message.Text = content.Text
	}
	if strings.TrimSpace(message.EventID) == "" || strings.TrimSpace(message.UserID) == "" || strings.TrimSpace(message.Text) == "" {
		return Message{}, fmt.Errorf("event_id, user_id and text are required")
	}
	return message, nil
}
