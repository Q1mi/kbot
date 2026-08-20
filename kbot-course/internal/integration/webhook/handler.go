// Package webhook 验证并解析通用 Webhook。
package webhook

import (
	"errors"
	"net/http"
)

var ErrNotImplemented = errors.New("webhook integration is implemented in 21-end")

func NewHandler(string, func([]byte) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, ErrNotImplemented.Error(), http.StatusNotImplemented)
	})
}
