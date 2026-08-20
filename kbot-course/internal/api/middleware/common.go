package middleware

import "net/http"

type Func func(http.Handler) http.Handler

func Chain(handler http.Handler, middlewares ...Func) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
