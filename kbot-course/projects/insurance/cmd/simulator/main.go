package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Q1mi/kbot-course/insurance/internal/httpapi"
	"github.com/Q1mi/kbot-course/insurance/internal/simulator"
)

func main() {
	address := os.Getenv("INSURANCE_ADDR")
	if address == "" {
		address = ":8092"
	}
	server := &http.Server{Addr: address, Handler: httpapi.New(simulator.NewSeeded()), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("insurance simulator listening on %s", address)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
