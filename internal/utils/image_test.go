package utils

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchDecodeImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, img)
	}))
	defer server.Close()

	res, err := FetchDecodeImage(context.Background(), server.URL, ImageOptions{MaxBytes: 1024 * 1024, MaxPixels: 1000, DownSampleSize: 4, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 10 || res.Height != 8 {
		t.Fatalf("original size = %dx%d", res.Width, res.Height)
	}
	if res.Image.Bounds().Dx() != 4 {
		t.Fatalf("downsample width = %d", res.Image.Bounds().Dx())
	}
}

func TestPixelsSkipBackground(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	all := Pixels(img, false)
	skipped := Pixels(img, true)
	if len(all) != 400 {
		t.Fatalf("all pixels = %d", len(all))
	}
	if len(skipped) != 256 {
		t.Fatalf("skipped pixels = %d", len(skipped))
	}
}
