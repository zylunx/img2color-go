package middleware

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		entry := map[string]interface{}{
			"ts":       time.Now().UTC().Format(time.RFC3339Nano),
			"method":   r.Method,
			"path":     r.URL.Path,
			"status":   sw.status,
			"duration": time.Since(start).Milliseconds(),
			"ip":       ClientIP(r),
		}
		if b, err := json.Marshal(entry); err == nil {
			log.Println(string(b))
		}
	})
}
