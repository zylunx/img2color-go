package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"img2color/internal/utils"
)

type RateLimiter struct {
	limit int
	mu    sync.Mutex
	hits  map[string]*ipWindow
}

type ipWindow struct {
	start time.Time
	count int
}

func NewRateLimiter(limit int) *RateLimiter {
	if limit <= 0 {
		limit = 60
	}
	return &RateLimiter{limit: limit, hits: make(map[string]*ipWindow)}
}

func (r *RateLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ip := ClientIP(req)
		now := time.Now()
		r.mu.Lock()
		win := r.hits[ip]
		if win == nil || now.Sub(win.start) >= time.Minute {
			win = &ipWindow{start: now}
			r.hits[ip] = win
		}
		win.count++
		allowed := win.count <= r.limit
		r.cleanup(now)
		r.mu.Unlock()
		if !allowed {
			utils.Error(w, http.StatusForbidden, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *RateLimiter) cleanup(now time.Time) {
	for ip, win := range r.hits {
		if now.Sub(win.start) > 2*time.Minute {
			delete(r.hits, ip)
		}
	}
}

func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
