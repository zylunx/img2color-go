package service

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"img2color/config"
	"img2color/internal/cache"
)

func TestAPIExtractsColor(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 6, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 6; x++ {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			}
		}
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, img)
	}))
	defer imageServer.Close()

	svc := New(testConfig(), cache.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/api?img="+imageServer.URL+"&format=hex&colors=1", nil)
	rec := httptest.NewRecorder()
	svc.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Code int       `json:"code"`
		Data ColorData `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || resp.Data.Dominant != "#FF0000" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAPIMissingImage(t *testing.T) {
	svc := New(testConfig(), cache.NoopStore{})
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	svc.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func testConfig() config.Config {
	return config.Config{
		AllowedReferers:   []string{"*"},
		MaxImageSizeBytes: 1024 * 1024,
		MaxImagePixels:    1024,
		DownSampleSize:    400,
		CacheTTL:          time.Minute,
		RequestTimeout:    time.Second,
		ExtractTimeout:    time.Second,
		RateLimitPerMin:   100,
		MaxConcurrent:     5,
	}
}
