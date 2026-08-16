// Package sandbox 使用 Docker 一次性容器隔离执行 Python 和 Bash 代码。
package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Config 定义 Runner 统一控制的资源与隔离边界。
type Config struct {
	PythonImage    string
	BashImage      string
	NetworkMode    string
	MemoryMB       int
	CPUs           float64
	PidsLimit      int
	Timeout        time.Duration
	TmpfsMB        int
	MaxCodeBytes   int
	MaxOutputBytes int
	MaxConcurrent  int
}

func DefaultConfig() Config {
	return Config{
		PythonImage:    "python:3.12.11-slim-bookworm@sha256:519591d6871b7bc437060736b9f7456b8731f1499a57e22e6c285135ae657bf7",
		BashImage:      "busybox:1.37.0@sha256:9db7b59979c38555a39def84a31fb98b5296952f9e3afd4f6f11f05b07adfab0",
		NetworkMode:    "none",
		MemoryMB:       256,
		CPUs:           0.5,
		PidsLimit:      64,
		Timeout:        30 * time.Second,
		TmpfsMB:        64,
		MaxCodeBytes:   64 << 10,
		MaxOutputBytes: 1 << 20,
		MaxConcurrent:  4,
	}
}

type Result struct {
	ExecutionID     string `json:"execution_id"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	TimedOut        bool   `json:"timed_out"`
	OutputTruncated bool   `json:"output_truncated"`
}

type Sandbox struct {
	cfg        Config
	dockerPath string
	semaphore  chan struct{}
}

func New(cfg Config) *Sandbox {
	cfg = normalizeConfig(cfg)
	path, _ := exec.LookPath("docker")
	return &Sandbox{cfg: cfg, dockerPath: path, semaphore: make(chan struct{}, cfg.MaxConcurrent)}
}

func normalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.PythonImage == "" {
		cfg.PythonImage = defaults.PythonImage
	}
	if cfg.BashImage == "" {
		cfg.BashImage = defaults.BashImage
	}
	if cfg.NetworkMode == "" {
		cfg.NetworkMode = defaults.NetworkMode
	}
	if cfg.MemoryMB <= 0 {
		cfg.MemoryMB = defaults.MemoryMB
	}
	if cfg.CPUs <= 0 {
		cfg.CPUs = defaults.CPUs
	}
	if cfg.PidsLimit <= 0 {
		cfg.PidsLimit = defaults.PidsLimit
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	if cfg.TmpfsMB <= 0 {
		cfg.TmpfsMB = defaults.TmpfsMB
	}
	if cfg.MaxCodeBytes <= 0 {
		cfg.MaxCodeBytes = defaults.MaxCodeBytes
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = defaults.MaxConcurrent
	}
	return cfg
}

func (s *Sandbox) Available() bool { return s.dockerPath != "" }

var ErrDockerUnavailable = errors.New("sandbox: docker is unavailable")
var ErrCapacity = errors.New("sandbox: runner is at capacity")

const sandboxLabel = "kbot.course.sandbox=true"

// Prepare 在监听流量前清理上次异常退出遗留的容器，并预拉取固定 digest 镜像。
func (s *Sandbox) Prepare(ctx context.Context) error {
	if !s.Available() {
		return ErrDockerUnavailable
	}
	if err := s.dockerInfo(ctx); err != nil {
		return err
	}
	output, err := exec.CommandContext(ctx, s.dockerPath, "ps", "-aq", "--filter", "label="+sandboxLabel).Output()
	if err != nil {
		return fmt.Errorf("sandbox: list orphan containers: %w", err)
	}
	if ids := strings.Fields(string(output)); len(ids) > 0 {
		args := append([]string{"rm", "-f"}, ids...)
		if cleanup, err := exec.CommandContext(ctx, s.dockerPath, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("sandbox: remove orphan containers: %w: %s", err, strings.TrimSpace(string(cleanup)))
		}
	}
	seen := make(map[string]struct{}, 2)
	for _, image := range []string{s.cfg.PythonImage, s.cfg.BashImage} {
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		if output, err := exec.CommandContext(ctx, s.dockerPath, "pull", image).CombinedOutput(); err != nil {
			return fmt.Errorf("sandbox: pull image %s: %w: %s", image, err, strings.TrimSpace(string(output)))
		}
	}
	return s.Check(ctx)
}

// Check 同时验证 Docker CLI 和 daemon。
func (s *Sandbox) Check(ctx context.Context) error {
	if !s.Available() {
		return ErrDockerUnavailable
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.dockerInfo(checkCtx); err != nil {
		return err
	}
	for _, image := range []string{s.cfg.PythonImage, s.cfg.BashImage} {
		if output, err := exec.CommandContext(checkCtx, s.dockerPath, "image", "inspect", image).CombinedOutput(); err != nil {
			return fmt.Errorf("sandbox: image %s is not ready: %w: %s", image, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func (s *Sandbox) dockerInfo(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, s.dockerPath, "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandbox: docker daemon unavailable: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Sandbox) Run(ctx context.Context, language, code string) (string, error) {
	result, err := s.Execute(ctx, language, code)
	return result.Stdout, err
}

func (s *Sandbox) Execute(ctx context.Context, language, code string) (Result, error) {
	started := time.Now()
	result := Result{ExecutionID: newExecutionID(), ExitCode: -1}
	if !s.Available() {
		return result, ErrDockerUnavailable
	}
	if code == "" {
		return result, errors.New("sandbox: code is required")
	}
	if len(code) > s.cfg.MaxCodeBytes {
		return result, fmt.Errorf("sandbox: code exceeds %d bytes", s.cfg.MaxCodeBytes)
	}

	args, err := s.buildArgs(language)
	if err != nil {
		return result, err
	}
	select {
	case <-ctx.Done():
		return result, ctx.Err()
	default:
	}
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	default:
		return result, ErrCapacity
	}

	runCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	containerName := "kbot-sandbox-" + result.ExecutionID
	args = append(args[:1], append([]string{"--name", containerName}, args[1:]...)...)
	command := exec.CommandContext(runCtx, s.dockerPath, args...)
	command.Stdin = strings.NewReader(code)
	stdout := newLimitedBuffer(s.cfg.MaxOutputBytes)
	stderr := newLimitedBuffer(s.cfg.MaxOutputBytes)
	command.Stdout = &stdout
	command.Stderr = &stderr

	err = command.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.OutputTruncated = stdout.truncated || stderr.truncated
	result.DurationMS = time.Since(started).Milliseconds()
	if exitError, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitError.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	if runCtx.Err() != nil {
		result.TimedOut = runCtx.Err() == context.DeadlineExceeded
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, s.dockerPath, "rm", "-f", containerName).Run()
		if result.TimedOut {
			return result, fmt.Errorf("sandbox: execution timed out after %s", s.cfg.Timeout)
		}
		return result, fmt.Errorf("sandbox: execution canceled: %w", runCtx.Err())
	}
	if err != nil {
		return result, fmt.Errorf("sandbox: execution failed: %v: %s", err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

// buildArgs 将安全边界映射为 docker run 参数，单元测试无需启动容器。
func (s *Sandbox) buildArgs(language string) ([]string, error) {
	args := []string{
		"run", "--rm", "-i",
		"--label", sandboxLabel,
		"--network", s.cfg.NetworkMode,
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=" + strconv.Itoa(s.cfg.TmpfsMB) + "m",
		"--memory", strconv.Itoa(s.cfg.MemoryMB) + "m",
		"--cpus", strconv.FormatFloat(s.cfg.CPUs, 'f', -1, 64),
		"--pids-limit", strconv.Itoa(s.cfg.PidsLimit),
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--user", "65534:65534",
		"--workdir", "/tmp",
		"--env", "HOME=/tmp",
		"--env", "PYTHONDONTWRITEBYTECODE=1",
		"--ulimit", "nofile=128:128",
	}
	switch language {
	case "python", "code_run_python":
		args = append(args, s.cfg.PythonImage, "python", "-c", "import sys; exec(sys.stdin.read())")
	case "bash", "sh", "code_run_bash":
		args = append(args, s.cfg.BashImage, "sh", "-s")
	default:
		return nil, fmt.Errorf("sandbox: unsupported language %q", language)
	}
	return args, nil
}

func newExecutionID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

type limitedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func newLimitedBuffer(limit int) limitedBuffer { return limitedBuffer{limit: limit} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	originalLength := len(p)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return originalLength, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, err := io.Copy(&b.Buffer, bytes.NewReader(p))
	return originalLength, err
}
