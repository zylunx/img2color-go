package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"

	"img2color/api"
)

// main starts a local HTTP server that reuses the same handler as the
// Vercel Serverless function. Vercel only builds api/**/*.go, so this
// entry point is ignored in production and used only for local runs.
func main() {
	loadDotEnv(".env")

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", api.Handler)

	addr := ":" + port
	log.Printf("img2color listening on http://localhost:%s", port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// loadDotEnv loads simple KEY=VALUE pairs from a .env file if it exists.
// Existing environment variables take precedence, so nothing is overwritten.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}
