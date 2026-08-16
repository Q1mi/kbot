package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestBuildArgsSecurityFlags(t *testing.T) {
	s := New(DefaultConfig())
	args, err := s.buildArgs("python")
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	joined := strings.Join(args, " ")

	// 隔离边界必须全部出现在 docker run 参数里。
	for _, want := range []string{
		"run", "--rm", "-i",
		"--network none",
		"--read-only",
		"--tmpfs /tmp:rw,noexec,nosuid,size=64m",
		"--memory 256m",
		"--cpus 0.5",
		"--pids-limit 64",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--user 65534:65534",
		"--workdir /tmp",
		"--env HOME=/tmp",
		"--env PYTHONDONTWRITEBYTECODE=1",
		"--ulimit nofile=128:128",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected docker args to contain %q, got: %s", want, joined)
		}
	}
}

func TestLimitedBufferCapsOutput(t *testing.T) {
	buffer := newLimitedBuffer(4)
	if n, err := buffer.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if got := buffer.String(); got != "abcd" || !buffer.truncated {
		t.Fatalf("buffer = %q truncated=%t", got, buffer.truncated)
	}
}

func TestBuildArgsLanguages(t *testing.T) {
	s := New(DefaultConfig())

	pyArgs, err := s.buildArgs("python")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(pyArgs, " "), "python") {
		t.Error("python args missing python interpreter")
	}

	bashArgs, err := s.buildArgs("bash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(bashArgs, " "), "sh") {
		t.Error("bash args missing shell")
	}

	if _, err := s.buildArgs("ruby"); err == nil {
		t.Error("expected error for unsupported language")
	}
}

func TestRunWithoutDocker(t *testing.T) {
	// 强制构造一个 docker 不可用的沙箱，验证优雅降级而非 panic。
	s := &Sandbox{cfg: DefaultConfig(), dockerPath: ""}
	if s.Available() {
		t.Skip("docker present; skipping unavailable-path test")
	}
	_, err := s.Run(context.Background(), "python", "print(1)")
	if err != ErrDockerUnavailable {
		t.Fatalf("expected ErrDockerUnavailable, got %v", err)
	}
}
