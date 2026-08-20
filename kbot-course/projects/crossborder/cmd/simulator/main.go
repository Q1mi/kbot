package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Q1mi/kbot-crossborder/internal/httpapi"
	"github.com/Q1mi/kbot-crossborder/internal/service"
)

func main() {
	addr := os.Getenv("CROSSBORDER_ADDR")
	if addr == "" {
		addr = ":8091"
	}
	server := &http.Server{
		Addr: addr, Handler: httpapi.New(service.NewSeeded()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("crossborder simulator listening on %s", addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
