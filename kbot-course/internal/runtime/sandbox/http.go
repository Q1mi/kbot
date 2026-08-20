package sandbox

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	executionsPath   = "/v1/executions"
	maxRequestBytes  = 128 << 10
	maxResponseBytes = 8 << 20
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

type Handler struct {
	backend executionBackend
	token   string
}

func NewHandler(backend executionBackend, token string) http.Handler {
	handler := &Handler{backend: backend, token: token}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.health)
	mux.HandleFunc("/readyz", handler.ready)
	mux.HandleFunc(executionsPath, handler.execute)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := h.backend.Check(request.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) execute(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.token == "" || !validBearerToken(request.Header.Get("Authorization"), h.token) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input ExecuteRequest
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request: " + err.Error()})
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain one JSON object"})
		return
	}
	result, err := h.backend.Execute(request.Context(), input.Language, input.Code)
	log.Printf("sandbox execution id=%s language=%q exit_code=%d duration_ms=%d timed_out=%t truncated=%t error=%t",
		result.ExecutionID, input.Language, result.ExitCode, result.DurationMS, result.TimedOut, result.OutputTruncated, err != nil)
	response := ExecuteResponse{Result: result}
	if err != nil {
		response.Error = err.Error()
	}
	if errors.Is(err, ErrCapacity) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, response)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func validBearerToken(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
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
	return &Client{baseURL: parsed.String(), token: token, http: &http.Client{Timeout: 40 * time.Second}}, nil
}

func (c *Client) Run(ctx context.Context, language, code string) (string, error) {
	result, err := c.Execute(ctx, language, code)
	return result.Stdout, err
}

func (c *Client) Execute(ctx context.Context, language, code string) (Result, error) {
	body, err := json.Marshal(ExecuteRequest{Language: language, Code: code})
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+executionsPath, strings.NewReader(string(body)))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("call sandbox runner: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxResponseBytes)
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(limited)
		return Result{}, fmt.Errorf("sandbox runner returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var payload ExecuteResponse
	if err := json.NewDecoder(limited).Decode(&payload); err != nil {
		return Result{}, fmt.Errorf("decode sandbox runner response: %w", err)
	}
	if payload.Error != "" {
		return payload.Result, errors.New(payload.Error)
	}
	return payload.Result, nil
}

func (c *Client) Check(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("sandbox runner readiness: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
		return fmt.Errorf("sandbox runner readiness returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	return nil
}
