package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"img2color/config"
	"img2color/internal/cache"
	palette "img2color/internal/color"
	"img2color/internal/middleware"
	"img2color/internal/utils"
)

type Service struct {
	cfg       config.Config
	cache     cache.Store
	limiter   *middleware.RateLimiter
	sem       chan struct{}
	requests  atomic.Uint64
	cacheHits atomic.Uint64
	errors    atomic.Uint64
}

type ColorItem struct {
	Color interface{} `json:"color"`
	Ratio float64     `json:"ratio"`
}

type ColorData struct {
	Dominant interface{} `json:"dominant"`
	Palette  []ColorItem `json:"palette"`
	Format   string      `json:"format"`
	Width    int         `json:"width"`
	Height   int         `json:"height"`
}

func New(cfg config.Config, store cache.Store) *Service {
	if store == nil {
		store = cache.NoopStore{}
	}
	return &Service{
		cfg:     cfg,
		cache:   store,
		limiter: middleware.NewRateLimiter(cfg.RateLimitPerMin),
		sem:     make(chan struct{}, cfg.MaxConcurrent),
	}
}

func (s *Service) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api", s.handleAPI)
	mux.HandleFunc("/", s.handleAPI)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	var h http.Handler = mux
	h = s.limiter.Wrap(h)
	h = middleware.RefererGuard(s.cfg.AllowedReferers)(h)
	h = middleware.Logger(h)
	return h
}

func (s *Service) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		utils.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.requests.Add(1)
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		s.errors.Add(1)
		utils.Error(w, http.StatusServiceUnavailable, "too many concurrent requests")
		return
	}

	req, err := parseRequest(r, s.cfg)
	if err != nil {
		s.errors.Add(1)
		utils.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	key := cache.Key(req.imgURL, req.format, req.colors, req.maxWidth, req.skipBackground, req.algorithm == "kmeans")
	var cached ColorData
	if err := s.cache.Get(r.Context(), key, &cached); err == nil {
		s.cacheHits.Add(1)
		utils.WriteJSON(w, http.StatusOK, 0, "success", cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout+s.cfg.ExtractTimeout)
	defer cancel()
	img, err := utils.FetchDecodeImage(ctx, req.imgURL, utils.ImageOptions{
		MaxBytes:       s.cfg.MaxImageSizeBytes,
		MaxPixels:      s.cfg.MaxImagePixels,
		DownSampleSize: req.maxWidth,
		Timeout:        s.cfg.RequestTimeout,
	})
	if err != nil {
		s.errors.Add(1)
		writeImageError(w, err)
		return
	}

	extractCtx, extractCancel := context.WithTimeout(ctx, s.cfg.ExtractTimeout)
	defer extractCancel()
	resultCh := make(chan extractResult, 1)
	go func() {
		pixels := utils.Pixels(img.Image, req.skipBackground)
		extractor := chooseExtractor(req.algorithm)
		swatches, err := extractor.Extract(pixels, req.colors)
		resultCh <- extractResult{swatches: swatches, err: err}
	}()

	var result extractResult
	select {
	case <-extractCtx.Done():
		s.errors.Add(1)
		utils.Error(w, http.StatusGatewayTimeout, "image extraction timeout")
		return
	case result = <-resultCh:
	}
	if result.err != nil {
		s.errors.Add(1)
		utils.Error(w, http.StatusInternalServerError, result.err.Error())
		return
	}
	data, err := buildResponse(result.swatches, req.format, img.Width, img.Height)
	if err != nil {
		s.errors.Add(1)
		utils.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.cache.Set(r.Context(), key, data, s.cfg.CacheTTL)
	utils.WriteJSON(w, http.StatusOK, 0, "success", data)
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	total := s.requests.Load()
	hits := s.cacheHits.Load()
	ratio := 0.0
	if total > 0 {
		ratio = float64(hits) / float64(total)
	}
	utils.WriteJSON(w, http.StatusOK, 0, "ok", map[string]interface{}{
		"status":          "up",
		"requests":        total,
		"cacheHits":       hits,
		"cacheHitRate":    ratio,
		"recentErrors":    s.errors.Load(),
		"maxConcurrent":   s.cfg.MaxConcurrent,
		"rateLimitPerMin": s.cfg.RateLimitPerMin,
	})
}

func (s *Service) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(
		"# TYPE img2color_requests_total counter\nimg2color_requests_total " + strconv.FormatUint(s.requests.Load(), 10) + "\n" +
			"# TYPE img2color_cache_hits_total counter\nimg2color_cache_hits_total " + strconv.FormatUint(s.cacheHits.Load(), 10) + "\n" +
			"# TYPE img2color_errors_total counter\nimg2color_errors_total " + strconv.FormatUint(s.errors.Load(), 10) + "\n",
	))
}

type requestParams struct {
	imgURL         string
	format         string
	colors         int
	maxWidth       int
	skipBackground bool
	algorithm      string
}

func parseRequest(r *http.Request, cfg config.Config) (requestParams, error) {
	q := r.URL.Query()
	imgURL := strings.TrimSpace(q.Get("img"))
	if imgURL == "" {
		return requestParams{}, errors.New("missing img parameter")
	}
	format := strings.ToLower(strings.TrimSpace(q.Get("format")))
	if format == "" {
		format = "hex"
	}
	colors := parseBoundedInt(q.Get("colors"), 1, 1, 10)
	maxWidth := parseBoundedInt(q.Get("maxWidth"), cfg.DownSampleSize, 32, 4096)
	skipBackground, _ := strconv.ParseBool(q.Get("skipBackground"))
	algorithm := strings.ToLower(strings.TrimSpace(q.Get("algorithm")))
	if algorithm == "" {
		algorithm = "mediancut"
	}
	if algorithm != "mediancut" && algorithm != "kmeans" {
		return requestParams{}, errors.New("unsupported algorithm")
	}
	return requestParams{imgURL: imgURL, format: format, colors: colors, maxWidth: maxWidth, skipBackground: skipBackground, algorithm: algorithm}, nil
}

func parseBoundedInt(raw string, fallback, minV, maxV int) int {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

type extractResult struct {
	swatches []palette.Swatch
	err      error
}

func chooseExtractor(algorithm string) palette.Extractor {
	if algorithm == "kmeans" {
		return palette.KMeans{}
	}
	return palette.MedianCut{}
}

func buildResponse(swatches []palette.Swatch, format string, width, height int) (ColorData, error) {
	items := make([]ColorItem, 0, len(swatches))
	for _, sw := range swatches {
		formatted, err := palette.Format(sw.Color, format)
		if err != nil {
			return ColorData{}, err
		}
		items = append(items, ColorItem{Color: formatted, Ratio: roundRatio(sw.Ratio)})
	}
	if len(items) == 0 {
		return ColorData{}, errors.New("no color extracted")
	}
	return ColorData{Dominant: items[0].Color, Palette: items, Format: format, Width: width, Height: height}, nil
}

func roundRatio(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}

func writeImageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		utils.Error(w, http.StatusGatewayTimeout, "image download timeout")
	case errors.Is(err, utils.ErrInvalidImageURL):
		utils.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, utils.ErrImageDownloadNotFound):
		utils.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, utils.ErrImageTooLarge), errors.Is(err, utils.ErrImagePixelsExceeded):
		utils.Error(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, utils.ErrUnsupportedImageType):
		utils.Error(w, http.StatusUnsupportedMediaType, err.Error())
	default:
		utils.Error(w, http.StatusInternalServerError, err.Error())
	}
}
