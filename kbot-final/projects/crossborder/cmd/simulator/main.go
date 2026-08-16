package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Q1mi/kbot-crossborder/internal/httpapi"
	"github.com/Q1mi/kbot-crossborder/internal/service"
)

func main() {
	addr := os.Getenv("CROSSBORDER_ADDR")
	if addr == "" {
		addr = ":8091"
	}
	log.Printf("crossborder simulator listening on %s", addr)
	if err := http.ListenAndServe(addr, httpapi.New(service.NewSeeded())); err != nil {
		log.Fatal(err)
	}
}
