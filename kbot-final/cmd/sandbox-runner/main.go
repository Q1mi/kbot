// Command sandbox-runner 在独立容器中管理 Docker 代码执行沙箱。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/sandbox"
)

func main() {
	addr := getenv("KBOT_SANDBOX_RUNNER_ADDR", ":8081")
	token := os.Getenv("KBOT_SANDBOX_RUNNER_TOKEN")
	if len(token) < 32 {
		log.Fatal("KBOT_SANDBOX_RUNNER_TOKEN 长度至少需要32字符")
	}

	cfg := sandbox.DefaultConfig()
	cfg.PythonImage = getenv("KBOT_SANDBOX_PYTHON_IMAGE", cfg.PythonImage)
	cfg.BashImage = getenv("KBOT_SANDBOX_BASH_IMAGE", cfg.BashImage)
	cfg.MemoryMB = getenvInt("KBOT_SANDBOX_MEMORY_MB", cfg.MemoryMB)
	cfg.CPUs = getenvFloat("KBOT_SANDBOX_CPUS", cfg.CPUs)
	cfg.PidsLimit = getenvInt("KBOT_SANDBOX_PIDS_LIMIT", cfg.PidsLimit)
	cfg.TmpfsMB = getenvInt("KBOT_SANDBOX_TMPFS_MB", cfg.TmpfsMB)
	cfg.MaxCodeBytes = getenvInt("KBOT_SANDBOX_MAX_CODE_BYTES", cfg.MaxCodeBytes)
	cfg.MaxOutputBytes = getenvInt("KBOT_SANDBOX_MAX_OUTPUT_BYTES", cfg.MaxOutputBytes)
	cfg.MaxConcurrent = getenvInt("KBOT_SANDBOX_MAX_CONCURRENT", cfg.MaxConcurrent)
	cfg.Timeout = getenvDuration("KBOT_SANDBOX_TIMEOUT", cfg.Timeout)
	// 网络策略由 Runner 固定为 none，执行请求不能覆盖。
	cfg.NetworkMode = "none"

	runner := sandbox.New(cfg)
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	if err := runner.Prepare(checkCtx); err != nil {
		checkCancel()
		log.Fatalf("sandbox runner preflight: %v", err)
	}
	checkCancel()

	server := &http.Server{
		Addr:              addr,
		Handler:           sandbox.NewHandler(runner, token),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      cfg.Timeout + 30*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Printf("sandbox runner listening on %s (memory=%dMB cpus=%g pids=%d timeout=%s concurrency=%d)",
			addr, cfg.MemoryMB, cfg.CPUs, cfg.PidsLimit, cfg.Timeout, cfg.MaxConcurrent)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("sandbox runner: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func getenvFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
