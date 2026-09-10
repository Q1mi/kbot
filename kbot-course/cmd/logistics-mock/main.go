package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Q1mi/kbot/internal/demo/logistics"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8082", "listen address")
	publicURL := flag.String("public-url", "http://127.0.0.1:8082", "address advertised in AgentCard")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: *addr, Handler: logistics.NewHandler(*publicURL), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("物流 Mock：%s/.well-known/agent-card.json，版本 %s；订单 CB-1001 / CB-1002 / CB-1003", *publicURL, logistics.Version)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
