package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"img2color/internal/color"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

var (
	ErrInvalidImageURL       = errors.New("invalid image url")
	ErrImageTooLarge         = errors.New("image file too large")
	ErrUnsupportedImageType  = errors.New("unsupported image type")
	ErrImagePixelsExceeded   = errors.New("image pixels exceed limit")
	ErrImageDownloadNotFound = errors.New("image not found")
)

type ImageResult struct {
	Image  image.Image
	Width  int
	Height int
}

type ImageOptions struct {
	MaxBytes       int64
	MaxPixels      int
	DownSampleSize int
	Timeout        time.Duration
}

func FetchDecodeImage(ctx context.Context, rawURL string, opt ImageOptions) (ImageResult, error) {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ImageResult{}, ErrInvalidImageURL
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ImageResult{}, err
	}
	req.Header.Set("User-Agent", "img2color/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ImageResult{}, context.DeadlineExceeded
		}
		return ImageResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ImageResult{}, ErrImageDownloadNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ImageResult{}, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	if resp.ContentLength > opt.MaxBytes {
		return ImageResult{}, ErrImageTooLarge
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return ImageResult{}, ErrUnsupportedImageType
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, opt.MaxBytes+1))
	if err != nil {
		return ImageResult{}, err
	}
	if int64(len(data)) > opt.MaxBytes {
		return ImageResult{}, ErrImageTooLarge
	}
	if isAVIF(data) {
		return ImageResult{}, ErrUnsupportedImageType
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImageResult{}, ErrUnsupportedImageType
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ImageResult{}, ErrUnsupportedImageType
	}
	if w*h > opt.MaxPixels {
		return ImageResult{}, ErrImagePixelsExceeded
	}
	if opt.DownSampleSize > 0 && (w > opt.DownSampleSize || h > opt.DownSampleSize) {
		img = ResizeNearest(img, opt.DownSampleSize)
	}
	return ImageResult{Image: img, Width: w, Height: h}, nil
}

func Pixels(img image.Image, skipBackground bool) []color.RGB {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	x0, y0, x1, y1 := b.Min.X, b.Min.Y, b.Max.X, b.Max.Y
	if skipBackground && w >= 20 && h >= 20 {
		dx := w / 10
		dy := h / 10
		x0 += dx
		x1 -= dx
		y0 += dy
		y1 -= dy
	}
	pixels := make([]color.RGB, 0, (x1-x0)*(y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			pixels = append(pixels, color.FromColor(img.At(x, y)))
		}
	}
	return pixels
}

func ResizeNearest(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if maxSide <= 0 || (sw <= maxSide && sh <= maxSide) {
		return src
	}
	var dw, dh int
	if sw >= sh {
		dw = maxSide
		dh = max(1, sh*maxSide/sw)
	} else {
		dh = maxSide
		dw = max(1, sw*maxSide/sh)
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := b.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			sx := b.Min.X + x*sw/dw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func isAVIF(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	return string(data[4:8]) == "ftyp" && strings.Contains(string(data[8:min(len(data), 32)]), "avif")
}
