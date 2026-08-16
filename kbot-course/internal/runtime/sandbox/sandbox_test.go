package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestBuildArgsContainsEverySecurityBoundary(t *testing.T) {
	runner := New(DefaultConfig())
	args, err := runner.buildArgs("python")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"run", "--rm", "-i", "--network none", "--read-only",
		"--tmpfs /tmp:rw,noexec,nosuid,size=64m", "--memory 256m",
		"--cpus 0.5", "--pids-limit 64", "--cap-drop ALL",
		"--security-opt no-new-privileges", "--user 65534:65534",
		"--workdir /tmp", "--env HOME=/tmp", "--ulimit nofile=128:128",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("docker args missing %q: %s", expected, joined)
		}
	}
}

func TestBuildArgsSupportsOnlyPythonAndBash(t *testing.T) {
	runner := New(DefaultConfig())
	if _, err := runner.buildArgs("python"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.buildArgs("bash"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.buildArgs("ruby"); err == nil {
		t.Fatal("expected unsupported language error")
	}
}

func TestLimitedBufferCapsOutput(t *testing.T) {
	buffer := newLimitedBuffer(4)
	if count, err := buffer.Write([]byte("abcdef")); err != nil || count != 6 {
		t.Fatalf("Write() = %d, %v", count, err)
	}
	if buffer.String() != "abcd" || !buffer.truncated {
		t.Fatalf("buffer = %q, truncated = %t", buffer.String(), buffer.truncated)
	}
}

func TestRunWithoutDockerReturnsExplicitError(t *testing.T) {
	runner := &Sandbox{cfg: DefaultConfig(), dockerPath: "", semaphore: make(chan struct{}, 1)}
	if _, err := runner.Run(context.Background(), "python", "print(1)"); err != ErrDockerUnavailable {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultImagesArePinnedAndCapacityFailsFast(t *testing.T) {
	config := DefaultConfig()
	if !strings.Contains(config.PythonImage, "@sha256:") || !strings.Contains(config.BashImage, "@sha256:") {
		t.Fatalf("sandbox images must be digest pinned: %#v", config)
	}
	runner := &Sandbox{cfg: config, dockerPath: "docker", semaphore: make(chan struct{}, 1)}
	runner.semaphore <- struct{}{}
	if _, err := runner.Execute(context.Background(), "python", "print(1)"); err != ErrCapacity {
		t.Fatalf("error = %v, want ErrCapacity", err)
	}
}
