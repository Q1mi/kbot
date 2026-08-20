// Package lark 将飞书事件转换为统一渠道消息。
package lark

import (
	"errors"
	"net/http"
)

var ErrNotImplemented = errors.New("Lark integration is implemented in 21-end")

func NewHandler(string, func([]byte) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, ErrNotImplemented.Error(), http.StatusNotImplemented)
	})
}
