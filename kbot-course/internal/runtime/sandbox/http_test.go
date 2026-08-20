package sandbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct {
	result Result
	err    error
	ready  error
}

func (f fakeBackend) Execute(_ context.Context, language, code string) (Result, error) {
	if language != "python" || code != "print(42)" {
		return Result{}, errors.New("unexpected input")
	}
	return f.result, f.err
}

func (f fakeBackend) Check(context.Context) error { return f.ready }

func TestHTTPClientCallsAuthenticatedRunner(t *testing.T) {
	server := httptest.NewServer(NewHandler(fakeBackend{result: Result{
		ExecutionID: "exec-1", Stdout: "42\n", ExitCode: 0,
	}}, "test-token"))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	output, err := client.Run(context.Background(), "python", "print(42)")
	if err != nil {
		t.Fatal(err)
	}
	if output != "42\n" {
		t.Fatalf("output = %q", output)
	}
	if err := client.Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestHTTPClientRejectsWrongToken(t *testing.T) {
	server := httptest.NewServer(NewHandler(fakeBackend{}, "test-token"))
	defer server.Close()
	client, err := NewClient(server.URL, "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Run(context.Background(), "python", "print(42)"); err == nil {
		t.Fatal("expected unauthorized error")
	}
}

func TestHTTPClientPreservesExecutionResultOnFailure(t *testing.T) {
	want := Result{ExecutionID: "exec-2", Stderr: "boom", ExitCode: 1}
	server := httptest.NewServer(NewHandler(fakeBackend{result: want, err: errors.New("execution failed")}, "test-token"))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Execute(context.Background(), "python", "print(42)")
	if err == nil || err.Error() != "execution failed" {
		t.Fatalf("error = %v", err)
	}
	if got.ExecutionID != want.ExecutionID || got.ExitCode != want.ExitCode || got.Stderr != want.Stderr {
		t.Fatalf("result = %#v", got)
	}
}

func TestRunnerReturns429WhenCapacityIsFull(t *testing.T) {
	server := httptest.NewServer(NewHandler(fakeBackend{err: ErrCapacity}, "test-token"))
	defer server.Close()
	request, err := http.NewRequest(http.MethodPost, server.URL+executionsPath, strings.NewReader(`{"language":"python","code":"print(42)"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
}
