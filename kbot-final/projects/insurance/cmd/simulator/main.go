package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Q1mi/kbot-insurance/internal/httpapi"
	"github.com/Q1mi/kbot-insurance/internal/service"
)

func main() {
	addr := os.Getenv("INSURANCE_ADDR")
	if addr == "" {
		addr = ":8092"
	}
	log.Printf("insurance simulator listening on %s", addr)
	if err := http.ListenAndServe(addr, httpapi.New(service.NewSeeded())); err != nil {
		log.Fatal(err)
	}
}
