package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Q1mi/kbot/internal/runtime/cache"
)

// Idempotency 复用同一用户、工作空间和路由下的成功响应。
// 只对带 key 的 POST/PUT 生效；只缓存 2xx 响应，后续请求会重新执行失败响应。
func Idempotency(store cache.IdemStore) func(http.Handler) http.Handler {
	locks := newKeyedLocks()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" || (r.Method != http.MethodPost && r.Method != http.MethodPut) {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > 256 {
				http.Error(w, "Idempotency-Key is too long", http.StatusBadRequest)
				return
			}
			requestHash, err := hashAndRestoreRequestBody(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}

			cacheKey := scopedIdempotencyKey(r, key)
			release := locks.acquire(cacheKey)
			defer release()
			if distributed, ok := store.(cache.DistributedLocker); ok {
				releaseDistributed, err := distributed.Lock(r.Context(), cacheKey, 30*time.Second)
				if err != nil {
					http.Error(w, "idempotency store unavailable", http.StatusServiceUnavailable)
					return
				}
				defer releaseDistributed()
			}

			got, found, err := store.Get(r.Context(), cacheKey)
			if err != nil {
				http.Error(w, "idempotency store unavailable", http.StatusServiceUnavailable)
				return
			}
			if found {
				if replayResponse(w, got, requestHash) {
					return
				}
			}

			rec := &bodyRecorder{ResponseWriter: w, buf: bytes.NewBuffer(nil), status: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.status >= 200 && rec.status < 300 {
				payload, err := json.Marshal(cachedResponse{
					Status: rec.status, Header: rec.Header().Clone(), Body: rec.buf.Bytes(), RequestHash: requestHash,
				})
				if err == nil {
					if err := store.Set(r.Context(), cacheKey, payload, 24*time.Hour); err != nil {
						log.Printf("persist idempotent response failed: key=%s error=%v", cacheKey, err)
					}
				}
			}
		})
	}
}

// bodyRecorder 同时写客户端并记录响应体，供幂等缓存。
type bodyRecorder struct {
	http.ResponseWriter
	buf         *bytes.Buffer
	status      int
	wroteHeader bool
}

func (r *bodyRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *bodyRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	_, _ = r.buf.Write(p)
	return r.ResponseWriter.Write(p)
}

func (r *bodyRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

type cachedResponse struct {
	Status      int         `json:"status"`
	Header      http.Header `json:"header"`
	Body        []byte      `json:"body"`
	RequestHash string      `json:"request_hash"`
}

func replayResponse(w http.ResponseWriter, payload []byte, requestHash string) bool {
	var cached cachedResponse
	if err := json.Unmarshal(payload, &cached); err != nil || cached.Status < 200 || cached.Status > 299 {
		return false
	}
	if cached.RequestHash != "" && cached.RequestHash != requestHash {
		http.Error(w, "Idempotency-Key was already used with a different request body", http.StatusConflict)
		return true
	}
	for name, values := range cached.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.Header().Set("X-Idempotent-Replay", "true")
	w.WriteHeader(cached.Status)
	_, _ = w.Write(cached.Body)
	return true
}

func hashAndRestoreRequestBody(r *http.Request) (string, error) {
	const maxIdempotentRequestBytes = 2 << 20
	if r.Body == nil {
		sum := sha256.Sum256(nil)
		return fmt.Sprintf("%x", sum), nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotentRequestBytes+1))
	if err != nil {
		return "", fmt.Errorf("read request body: %w", err)
	}
	if len(body) > maxIdempotentRequestBytes {
		return "", fmt.Errorf("idempotent request body is too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum), nil
}

func scopedIdempotencyKey(r *http.Request, key string) string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		GetUserIDFromContext(r.Context()),
		GetWorkspaceIDFromContext(r.Context()),
		r.Method,
		r.URL.Path,
		r.URL.RawQuery,
		key,
	)
	return fmt.Sprintf("http-idem:%x", sha256.Sum256([]byte(raw)))
}

type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

func newKeyedLocks() *keyedLocks {
	return &keyedLocks{locks: make(map[string]*keyedLock)}
}

func (l *keyedLocks) acquire(key string) func() {
	l.mu.Lock()
	lock := l.locks[key]
	if lock == nil {
		lock = &keyedLock{}
		l.locks[key] = lock
	}
	lock.refs++
	l.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		l.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}
