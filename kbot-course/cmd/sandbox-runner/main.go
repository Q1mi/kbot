// Command sandbox-runner 是独立的代码执行控制面。
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Q1mi/kbot/internal/runtime/sandbox"
)

func main() {
	token := os.Getenv("KBOT_SANDBOX_RUNNER_TOKEN")
	if len(token) < 32 {
		log.Fatal("KBOT_SANDBOX_RUNNER_TOKEN must contain at least 32 characters")
	}
	log.Fatal(http.ListenAndServe(":8081", sandbox.NewHandler(sandbox.New(sandbox.DefaultConfig()), token)))
}
