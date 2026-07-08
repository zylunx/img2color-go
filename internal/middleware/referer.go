package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"img2color/internal/utils"
)

func RefererGuard(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ref := r.Header.Get("Referer")
			if ref == "" || refererAllowed(ref, allowed) {
				next.ServeHTTP(w, r)
				return
			}
			utils.Error(w, http.StatusForbidden, "referer is not allowed")
		})
	}
}

func refererAllowed(ref string, allowed []string) bool {
	for _, pattern := range allowed {
		if pattern == "*" {
			return true
		}
	}
	u, err := url.Parse(ref)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, pattern := range allowed {
		p := strings.ToLower(strings.TrimSpace(pattern))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			suffix := strings.TrimPrefix(p, "*")
			if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(suffix, ".") {
				return true
			}
			continue
		}
		if host == p {
			return true
		}
	}
	return false
}
