package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type ExecuteRequest struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

type ExecuteResponse struct {
	Result Result `json:"result"`
	Error  string `json:"error,omitempty"`
}

type executionBackend interface {
	Execute(ctx context.Context, language, code string) (Result, error)
	Check(ctx context.Context) error
}

// NewHandler 的鉴权、严格解码和受控错误响应在 08-end 完成。
func NewHandler(executionBackend, string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, ErrNotImplemented.Error(), http.StatusNotImplemented)
	})
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid sandbox runner URL %q", baseURL)
	}
	if token == "" {
		return nil, errors.New("sandbox runner token is required")
	}
	return &Client{baseURL: parsed.String(), token: token, http: http.DefaultClient}, nil
}

func (c *Client) Run(context.Context, string, string) (string, error) {
	return "", ErrNotImplemented
}

func (c *Client) Check(context.Context) error { return ErrNotImplemented }
