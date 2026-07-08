package api

import (
	"net/http"
	"sync"

	"img2color/internal/bootstrap"
)

var (
	handlerOnce sync.Once
	handler     http.Handler
)

// Handler is the Vercel Serverless entry point.
func Handler(w http.ResponseWriter, r *http.Request) {
	handlerOnce.Do(func() {
		handler = bootstrap.NewService().Router()
	})
	handler.ServeHTTP(w, r)
}
