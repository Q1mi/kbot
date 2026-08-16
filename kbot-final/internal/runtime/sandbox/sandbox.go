// Package sandbox 用 Docker 在隔离容器里执行 code_run_python / code_run_bash。
//
// 设计文档 §4.3 / §7.7 选定 Docker 起容器跑代码。讲义 §14.2 用 docker/docker/client
// SDK 写 HostConfig；本实现改用 docker CLI（os/exec）驱动同一组安全边界——
// HostConfig 里的每一项都 1:1 映射到一个 docker run 标志：
//
//	ReadonlyRootfs           → --read-only
//	NetworkMode "none"       → --network none
//	Memory / NanoCPUs        → --memory / --cpus
//	PidsLimit                → --pids-limit
//	AutoRemove               → --rm
//	stdin 喂代码             → -i + 进程 stdin
//	墙钟超时                 → context.WithTimeout + 进程被 kill
//
// 这样既避免引入庞大的 Docker SDK 依赖树（利于 go build 复现），又完整保留了
// 沙箱隔离的教学点。换 SDK / firecracker 是部署期决定，收敛在本包一处即可。
package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Config 是沙箱的资源与隔离边界。
type Config struct {
	PythonImage    string
	BashImage      string
	NetworkMode    string        // 默认 "none"（禁网）
	MemoryMB       int           // 内存上限
	CPUs           float64       // CPU 上限
	PidsLimit      int           // 进程数上限
	Timeout        time.Duration // 墙钟超时
	TmpfsMB        int           // /tmp 上限
	MaxCodeBytes   int           // 单次代码大小上限
	MaxOutputBytes int           // stdout/stderr 各自的保留上限
	MaxConcurrent  int           // 单 Runner 最大并发执行数
}

// DefaultConfig 给出与讲义 §14.2 一致的保守默认值。
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

// Result 是一次容器执行的结构化结果。
type Result struct {
	ExecutionID     string `json:"execution_id"`
	ContainerName   string `json:"container_name"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	TimedOut        bool   `json:"timed_out"`
	OutputTruncated bool   `json:"output_truncated"`
}

// Sandbox 通过 docker CLI 执行代码。
type Sandbox struct {
	cfg        Config
	dockerPath string // docker 可执行文件路径；为空表示当前环境无 docker
	semaphore  chan struct{}
}

// New 创建沙箱。若环境里找不到 docker，仍返回实例，但 Run 会返回明确的错误
// （让上层与测试可以优雅降级，避免启动期崩溃）。
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

// Available 报告当前环境是否具备执行能力（docker 在 PATH 上）。
func (s *Sandbox) Available() bool { return s.dockerPath != "" }

// ErrDockerUnavailable 表示当前环境没有可用的 docker。
var ErrDockerUnavailable = errors.New("sandbox: docker 不可用（PATH 中找不到 docker）")
var ErrCapacity = errors.New("sandbox: runner capacity exhausted")

// Prepare 清理上次异常退出遗留的容器，并确保固定运行镜像已在 Docker daemon 中。
func (s *Sandbox) Prepare(ctx context.Context) error {
	if err := s.checkDaemon(ctx); err != nil {
		return err
	}
	if err := s.cleanupOrphans(ctx); err != nil {
		return fmt.Errorf("cleanup orphan sandbox containers: %w", err)
	}
	for _, image := range []string{s.cfg.PythonImage, s.cfg.BashImage} {
		if output, err := exec.CommandContext(ctx, s.dockerPath, "image", "inspect", image).CombinedOutput(); err != nil {
			log.Printf("sandbox image %s missing, pulling before readiness", image)
			if pullOutput, pullErr := exec.CommandContext(ctx, s.dockerPath, "pull", image).CombinedOutput(); pullErr != nil {
				return fmt.Errorf("pull sandbox image %s: %v: %s; inspect=%s", image, pullErr, strings.TrimSpace(string(pullOutput)), strings.TrimSpace(string(output)))
			}
		}
	}
	return nil
}

// Check 验证 Runner 到 Docker daemon 的完整连接。
func (s *Sandbox) Check(ctx context.Context) error {
	if err := s.checkDaemon(ctx); err != nil {
		return err
	}
	for _, image := range []string{s.cfg.PythonImage, s.cfg.BashImage} {
		if output, err := exec.CommandContext(ctx, s.dockerPath, "image", "inspect", image).CombinedOutput(); err != nil {
			return fmt.Errorf("sandbox image %s is not ready: %v: %s", image, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func (s *Sandbox) checkDaemon(ctx context.Context) error {
	if !s.Available() {
		return ErrDockerUnavailable
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(checkCtx, s.dockerPath, "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		return fmt.Errorf("sandbox: docker daemon 不可用: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *Sandbox) cleanupOrphans(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, s.dockerPath, "ps", "-aq", "--filter", "name=kbot-sandbox-").Output()
	if err != nil {
		return err
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	if output, err := exec.CommandContext(ctx, s.dockerPath, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Run 在隔离容器里执行 language(python/bash) 代码并返回 stdout。
func (s *Sandbox) Run(ctx context.Context, language, code string) (string, error) {
	result, err := s.Execute(ctx, language, code)
	return result.Stdout, err
}

// Execute 在一次性隔离容器中执行代码并返回结构化资源结果。
func (s *Sandbox) Execute(ctx context.Context, language, code string) (Result, error) {
	started := time.Now()
	result := Result{ExecutionID: newExecutionID(), ExitCode: -1}
	if !s.Available() {
		return result, ErrDockerUnavailable
	}
	if len(code) == 0 {
		return result, errors.New("sandbox: 代码不能为空")
	}
	if len(code) > s.cfg.MaxCodeBytes {
		return result, fmt.Errorf("sandbox: 代码超过 %d 字节上限", s.cfg.MaxCodeBytes)
	}

	args, err := s.buildArgs(language)
	if err != nil {
		return result, err
	}

	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		return result, ctx.Err()
	default:
		return result, ErrCapacity
	}

	runCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	containerName := "kbot-sandbox-" + result.ExecutionID
	result.ContainerName = containerName
	args = append(args[:1], append([]string{"--name", containerName}, args[1:]...)...)
	cmd := exec.CommandContext(runCtx, s.dockerPath, args...)
	cmd.Stdin = strings.NewReader(code) // 代码从 stdin 喂入，不落盘
	stdout := newLimitedBuffer(s.cfg.MaxOutputBytes)
	stderr := newLimitedBuffer(s.cfg.MaxOutputBytes)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.OutputTruncated = stdout.truncated || stderr.truncated
	result.DurationMS = time.Since(started).Milliseconds()
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else if err == nil {
		result.ExitCode = 0
	}
	if runCtx.Err() != nil {
		result.TimedOut = runCtx.Err() == context.DeadlineExceeded
		// docker 客户端被 context 终止后，显式清理可能仍在运行的容器。
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		cleanupOutput, cleanupErr := exec.CommandContext(cleanupCtx, s.dockerPath, "rm", "-f", containerName).CombinedOutput()
		if cleanupErr != nil {
			log.Printf("sandbox cleanup failed container=%s error=%v output=%s", containerName, cleanupErr, strings.TrimSpace(string(cleanupOutput)))
		}
		if result.TimedOut {
			return result, fmt.Errorf("sandbox: 执行超时（%s）", s.cfg.Timeout)
		}
		return result, fmt.Errorf("sandbox: 执行取消: %w", runCtx.Err())
	}
	if err != nil {
		return result, fmt.Errorf("sandbox: 执行失败: %v: %s", err, strings.TrimSpace(result.Stderr))
	}

	return result, nil
}

// buildArgs 构造 docker run 的参数；单独拎出来便于单元测试（无需真起容器）。
func (s *Sandbox) buildArgs(language string) ([]string, error) {
	netMode := s.cfg.NetworkMode
	if netMode == "" {
		netMode = "none"
	}

	args := []string{
		"run", "--rm", "-i",
		"--network", netMode,
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
		// 从 stdin 读全部代码后 exec，stdout 即返回值。
		args = append(args, s.cfg.PythonImage, "python", "-c",
			"import sys; exec(sys.stdin.read())")
	case "bash", "sh", "code_run_bash":
		args = append(args, s.cfg.BashImage, "sh", "-s")
	default:
		return nil, fmt.Errorf("sandbox: 不支持的语言 %q", language)
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
	originalLen := len(p)
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return originalLen, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, err := io.Copy(&b.Buffer, bytes.NewReader(p))
	return originalLen, err
}
