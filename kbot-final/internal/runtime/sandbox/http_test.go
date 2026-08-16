package sandbox

import (
	"context"
	"errors"
	"net/http/httptest"
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

func TestHTTPClientExecute(t *testing.T) {
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

func TestHTTPClientPreservesExecutionError(t *testing.T) {
	want := Result{ExecutionID: "exec-2", Stderr: "boom", ExitCode: 1}
	server := httptest.NewServer(NewHandler(fakeBackend{result: want, err: errors.New("execution failed")}, "test-token"))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Execute(context.Background(), "python", "print(42)")
	if err == nil || err.Error() != "execution failed" {
		t.Fatalf("err = %v", err)
	}
	if got.ExecutionID != want.ExecutionID || got.ExitCode != want.ExitCode || got.Stderr != want.Stderr {
		t.Fatalf("result = %#v", got)
	}
}
