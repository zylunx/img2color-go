package api

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	stdcolor "image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/gen2brain/avif"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

var (
	handlerOnce sync.Once
	handler     http.Handler
)

// Handler is the Vercel Serverless entry point.
func Handler(w http.ResponseWriter, r *http.Request) {
	handlerOnce.Do(func() {
		cfg := Load()
		handler = NewService(cfg, buildCache(cfg)).Router()
	})
	handler.ServeHTTP(w, r)
}

type Config struct {
	AllowedReferers   []string
	MaxImageSizeBytes int64
	MaxImagePixels    int
	DownSampleSize    int
	RedisAddress      string
	RedisPassword     string
	RedisDB           int
	UseRedisCache     bool
	MongoURI          string
	MongoDB           string
	UseMongoDB        bool
	CacheTTL          time.Duration
	RequestTimeout    time.Duration
	ExtractTimeout    time.Duration
	RateLimitPerMin   int
	MaxConcurrent     int
}

func Load() Config {
	return Config{
		AllowedReferers:   splitCSV(envString("ALLOWED_REFERERS", "*")),
		MaxImageSizeBytes: int64(envInt("MAX_IMAGE_SIZE_MB", 10)) * 1024 * 1024,
		MaxImagePixels:    envInt("MAX_IMAGE_PIXELS", 16777216),
		DownSampleSize:    envInt("DOWN_SAMPLE_SIZE", 400),
		RedisAddress:      os.Getenv("REDIS_ADDRESS"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		RedisDB:           envInt("REDIS_DB", 0),
		UseRedisCache:     envBool("USE_REDIS_CACHE", false),
		MongoURI:          os.Getenv("MONGO_URI"),
		MongoDB:           envString("MONGO_DB", "img2color"),
		UseMongoDB:        envBool("USE_MONGODB", false),
		CacheTTL:          time.Duration(envInt("CACHE_TTL_HOURS", 168)) * time.Hour,
		RequestTimeout:    time.Duration(envInt("REQUEST_TIMEOUT_SEC", 10)) * time.Second,
		ExtractTimeout:    time.Duration(envInt("EXTRACT_TIMEOUT_SEC", 5)) * time.Second,
		RateLimitPerMin:   envInt("RATE_LIMIT_PER_MIN", 60),
		MaxConcurrent:     envInt("MAX_CONCURRENT", 50),
	}
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}

type Service struct {
	cfg       Config
	cache     Store
	limiter   *RateLimiter
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

func NewService(cfg Config, store Store) *Service {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Service{
		cfg:     cfg,
		cache:   store,
		limiter: NewRateLimiter(cfg.RateLimitPerMin),
		sem:     make(chan struct{}, cfg.MaxConcurrent),
	}
}

func buildCache(cfg Config) Store {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if cfg.UseRedisCache && cfg.RedisAddress != "" {
		store := NewRedisStore(cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
		if err := store.client.Ping(ctx).Err(); err != nil {
			log.Printf("redis cache disabled: %v", err)
			_ = store.Close(ctx)
		} else {
			return store
		}
	}
	if cfg.UseMongoDB && cfg.MongoURI != "" {
		store, err := NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDB)
		if err != nil {
			log.Printf("mongodb cache disabled: %v", err)
		} else {
			return store
		}
	}
	return NewMemoryStore()
}

func (s *Service) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api", s.handleAPI)
	mux.HandleFunc("/", s.handleAPI)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	var h http.Handler = mux
	h = s.limiter.Wrap(h)
	h = RefererGuard(s.cfg.AllowedReferers)(h)
	h = Logger(h)
	return h
}

func (s *Service) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/api" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.requests.Add(1)
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		s.errors.Add(1)
		Error(w, http.StatusServiceUnavailable, "too many concurrent requests")
		return
	}

	req, err := parseRequest(r, s.cfg)
	if err != nil {
		s.errors.Add(1)
		Error(w, http.StatusBadRequest, err.Error())
		return
	}

	key := CacheKey(req.imgURL, req.format, req.colors, req.maxWidth, req.skipBackground, req.algorithm == "kmeans")
	var cached ColorData
	if err := s.cache.Get(r.Context(), key, &cached); err == nil {
		s.cacheHits.Add(1)
		WriteJSON(w, http.StatusOK, 0, "success", cached)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout+s.cfg.ExtractTimeout)
	defer cancel()
	img, err := FetchDecodeImage(ctx, req.imgURL, ImageOptions{
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
		pixels := Pixels(img.Image, req.skipBackground)
		extractor := chooseExtractor(req.algorithm)
		swatches, err := extractor.Extract(pixels, extractionLimit(req.colors))
		if err == nil {
			swatches = preferNonPureSwatches(swatches, req.colors)
		}
		resultCh <- extractResult{swatches: swatches, err: err}
	}()

	var result extractResult
	select {
	case <-extractCtx.Done():
		s.errors.Add(1)
		Error(w, http.StatusGatewayTimeout, "image extraction timeout")
		return
	case result = <-resultCh:
	}
	if result.err != nil {
		s.errors.Add(1)
		Error(w, http.StatusInternalServerError, result.err.Error())
		return
	}
	data, err := buildResponse(result.swatches, req.format, img.Width, img.Height)
	if err != nil {
		s.errors.Add(1)
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.cache.Set(r.Context(), key, data, s.cfg.CacheTTL)
	WriteJSON(w, http.StatusOK, 0, "success", data)
}

func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	total := s.requests.Load()
	hits := s.cacheHits.Load()
	ratio := 0.0
	if total > 0 {
		ratio = float64(hits) / float64(total)
	}
	WriteJSON(w, http.StatusOK, 0, "ok", map[string]interface{}{
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

func parseRequest(r *http.Request, cfg Config) (requestParams, error) {
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
	swatches []Swatch
	err      error
}

func chooseExtractor(algorithm string) Extractor {
	if algorithm == "kmeans" {
		return KMeans{}
	}
	return MedianCut{}
}

func extractionLimit(colors int) int {
	if colors < 1 {
		return 1
	}
	return colors + 2
}

func preferNonPureSwatches(swatches []Swatch, limit int) []Swatch {
	if limit <= 0 {
		limit = 1
	}
	preferred := make([]Swatch, 0, min(limit, len(swatches)))
	for _, sw := range swatches {
		if isPureBlackOrWhite(sw.Color) {
			continue
		}
		preferred = append(preferred, sw)
		if len(preferred) == limit {
			return preferred
		}
	}
	if len(preferred) > 0 {
		return preferred
	}
	if len(swatches) > limit {
		return swatches[:limit]
	}
	return swatches
}

func isPureBlackOrWhite(c RGB) bool {
	return (c.R == 0 && c.G == 0 && c.B == 0) || (c.R == 255 && c.G == 255 && c.B == 255)
}

func buildResponse(swatches []Swatch, format string, width, height int) (ColorData, error) {
	items := make([]ColorItem, 0, len(swatches))
	for _, sw := range swatches {
		formatted, err := Format(sw.Color, format)
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
		Error(w, http.StatusGatewayTimeout, "image download timeout")
	case errors.Is(err, ErrInvalidImageURL):
		Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrImageDownloadNotFound):
		Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrImageTooLarge), errors.Is(err, ErrImagePixelsExceeded):
		Error(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, ErrUnsupportedImageType):
		Error(w, http.StatusUnsupportedMediaType, err.Error())
	default:
		Error(w, http.StatusInternalServerError, err.Error())
	}
}

type APIResponse struct {
	Code    int         `json:"code"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{Code: code, Data: data, Message: message})
}

func Error(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, status, message, nil)
}

func RefererGuard(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ref := r.Header.Get("Referer")
			if ref == "" || refererAllowed(ref, allowed) {
				next.ServeHTTP(w, r)
				return
			}
			Error(w, http.StatusForbidden, "referer is not allowed")
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
			Error(w, http.StatusForbidden, "rate limit exceeded")
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

var ErrMiss = errors.New("cache miss")

type Store interface {
	Get(ctx context.Context, key string, dst interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Close(ctx context.Context) error
}

type NoopStore struct{}

func (NoopStore) Get(context.Context, string, interface{}) error { return ErrMiss }

func (NoopStore) Set(context.Context, string, interface{}, time.Duration) error {
	return nil
}

func (NoopStore) Close(context.Context) error { return nil }

type MemoryStore struct {
	mu    sync.RWMutex
	items map[string]memoryItem
}

type memoryItem struct {
	expireAt time.Time
	value    interface{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]memoryItem)}
}

func (m *MemoryStore) Get(_ context.Context, key string, dst interface{}) error {
	m.mu.RLock()
	item, ok := m.items[key]
	m.mu.RUnlock()
	if !ok || time.Now().After(item.expireAt) {
		return ErrMiss
	}
	return copyJSON(dst, item.value)
}

func (m *MemoryStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = memoryItem{expireAt: time.Now().Add(ttl), value: value}
	return nil
}

func (m *MemoryStore) Close(context.Context) error { return nil }

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr, password string, db int) *RedisStore {
	return &RedisStore{client: redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})}
}

func (r *RedisStore) Get(ctx context.Context, key string, dst interface{}) error {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return ErrMiss
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(val), dst)
}

func (r *RedisStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, b, ttl).Err()
}

func (r *RedisStore) Close(context.Context) error {
	return r.client.Close()
}

type MongoStore struct {
	client     *mongo.Client
	collection *mongo.Collection
}

type mongoDoc struct {
	ID       string      `bson:"_id"`
	ExpireAt time.Time   `bson:"expireAt"`
	Value    interface{} `bson:"value"`
}

func NewMongoStore(ctx context.Context, uri, db string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return &MongoStore{client: client, collection: client.Database(db).Collection("image_color_cache")}, nil
}

func (m *MongoStore) Get(ctx context.Context, key string, dst interface{}) error {
	var doc mongoDoc
	if err := m.collection.FindOne(ctx, bson.M{"_id": key, "expireAt": bson.M{"$gt": time.Now()}}).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return ErrMiss
		}
		return err
	}
	return copyJSON(dst, doc.Value)
}

func (m *MongoStore) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	_, err := m.collection.UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"value": value, "expireAt": time.Now().Add(ttl)}}, options.Update().SetUpsert(true))
	return err
}

func (m *MongoStore) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

func CacheKey(rawURL, format string, colors, maxWidth int, skipBackground, kmeans bool) string {
	sum := md5.Sum([]byte(rawURL))
	return hex.EncodeToString(sum[:]) + ":" + format + ":" + itoa(colors) + ":" + itoa(maxWidth) + ":" + boolStr(skipBackground) + ":" + boolStr(kmeans) + ":v2"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func boolStr(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func copyJSON(dst interface{}, src interface{}) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

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

func Pixels(img image.Image, skipBackground bool) []RGB {
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
	pixels := make([]RGB, 0, (x1-x0)*(y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			pixels = append(pixels, FromColor(img.At(x, y)))
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

type RGB struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
}

type HSL struct {
	H int `json:"h"`
	S int `json:"s"`
	L int `json:"l"`
}

type CMYK struct {
	C int `json:"c"`
	M int `json:"m"`
	Y int `json:"y"`
	K int `json:"k"`
}

type Swatch struct {
	Color RGB     `json:"-"`
	Ratio float64 `json:"ratio"`
}

type Extractor interface {
	Extract(pixels []RGB, n int) ([]Swatch, error)
}

func FromColor(c stdcolor.Color) RGB {
	r, g, b, _ := c.RGBA()
	return RGB{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
}

type MedianCut struct{}

type box struct {
	pixels []RGB
}

func (MedianCut) Extract(pixels []RGB, n int) ([]Swatch, error) {
	if len(pixels) == 0 {
		return nil, errors.New("no pixels to extract")
	}
	if n <= 0 {
		n = 1
	}
	boxes := []box{{pixels: append([]RGB(nil), pixels...)}}
	for len(boxes) < n {
		idx := largestBox(boxes)
		if idx < 0 || len(boxes[idx].pixels) < 2 {
			break
		}
		a, b := splitBox(boxes[idx])
		boxes = append(append(boxes[:idx], boxes[idx+1:]...), a, b)
	}
	total := float64(len(pixels))
	out := make([]Swatch, 0, len(boxes))
	for _, b := range boxes {
		out = append(out, Swatch{Color: average(b.pixels), Ratio: float64(len(b.pixels)) / total})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ratio > out[j].Ratio })
	return out, nil
}

func largestBox(boxes []box) int {
	best, score := -1, -1
	for i, b := range boxes {
		rng := channelRange(b.pixels)
		s := max3(rng[0], rng[1], rng[2]) * len(b.pixels)
		if s > score {
			best, score = i, s
		}
	}
	return best
}

func splitBox(b box) (box, box) {
	p := b.pixels
	rng := channelRange(p)
	channel := 0
	if rng[1] >= rng[0] && rng[1] >= rng[2] {
		channel = 1
	} else if rng[2] >= rng[0] && rng[2] >= rng[1] {
		channel = 2
	}
	sort.Slice(p, func(i, j int) bool {
		switch channel {
		case 0:
			return p[i].R < p[j].R
		case 1:
			return p[i].G < p[j].G
		default:
			return p[i].B < p[j].B
		}
	})
	mid := len(p) / 2
	return box{pixels: append([]RGB(nil), p[:mid]...)}, box{pixels: append([]RGB(nil), p[mid:]...)}
}

func channelRange(pixels []RGB) [3]int {
	minR, minG, minB := 255, 255, 255
	maxR, maxG, maxB := 0, 0, 0
	for _, p := range pixels {
		minR, maxR = min(minR, int(p.R)), max(maxR, int(p.R))
		minG, maxG = min(minG, int(p.G)), max(maxG, int(p.G))
		minB, maxB = min(minB, int(p.B)), max(maxB, int(p.B))
	}
	return [3]int{maxR - minR, maxG - minG, maxB - minB}
}

func average(pixels []RGB) RGB {
	var r, g, b int
	for _, p := range pixels {
		r += int(p.R)
		g += int(p.G)
		b += int(p.B)
	}
	l := len(pixels)
	return RGB{R: uint8(r / l), G: uint8(g / l), B: uint8(b / l)}
}

func max3(a, b, c int) int { return max(a, max(b, c)) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type KMeans struct {
	Iterations int
}

func (k KMeans) Extract(pixels []RGB, n int) ([]Swatch, error) {
	if len(pixels) == 0 {
		return nil, errors.New("no pixels to extract")
	}
	if n <= 0 {
		n = 1
	}
	if n > len(pixels) {
		n = len(pixels)
	}
	iterations := k.Iterations
	if iterations <= 0 {
		iterations = 12
	}
	centers := seedCenters(pixels, n)
	assignments := make([]int, len(pixels))
	for it := 0; it < iterations; it++ {
		sums := make([][4]int, n)
		for i, p := range pixels {
			idx := nearest(p, centers)
			assignments[i] = idx
			sums[idx][0] += int(p.R)
			sums[idx][1] += int(p.G)
			sums[idx][2] += int(p.B)
			sums[idx][3]++
		}
		for i, s := range sums {
			if s[3] == 0 {
				continue
			}
			centers[i] = RGB{R: uint8(s[0] / s[3]), G: uint8(s[1] / s[3]), B: uint8(s[2] / s[3])}
		}
	}
	counts := make([]int, n)
	for _, a := range assignments {
		counts[a]++
	}
	out := make([]Swatch, 0, n)
	total := float64(len(pixels))
	for i, c := range centers {
		if counts[i] > 0 {
			out = append(out, Swatch{Color: c, Ratio: float64(counts[i]) / total})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ratio > out[j].Ratio })
	return out, nil
}

func seedCenters(pixels []RGB, n int) []RGB {
	step := len(pixels) / n
	centers := make([]RGB, n)
	for i := range centers {
		centers[i] = pixels[min(i*step, len(pixels)-1)]
	}
	return centers
}

func nearest(p RGB, centers []RGB) int {
	best := 0
	bestDist := math.MaxFloat64
	for i, c := range centers {
		d := distance(p, c)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

func distance(a, b RGB) float64 {
	dr := float64(int(a.R) - int(b.R))
	dg := float64(int(a.G) - int(b.G))
	db := float64(int(a.B) - int(b.B))
	return dr*dr + dg*dg + db*db
}

func Format(c RGB, format string) (interface{}, error) {
	switch strings.ToLower(format) {
	case "", "hex":
		return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B), nil
	case "rgb":
		return c, nil
	case "hsl":
		return ToHSL(c), nil
	case "cmyk":
		return ToCMYK(c), nil
	default:
		return nil, fmt.Errorf("unsupported color format: %s", format)
	}
}

func ToHSL(c RGB) HSL {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	maxV := math.Max(r, math.Max(g, b))
	minV := math.Min(r, math.Min(g, b))
	l := (maxV + minV) / 2
	if maxV == minV {
		return HSL{H: 0, S: 0, L: int(math.Round(l * 100))}
	}
	d := maxV - minV
	var h float64
	if l > 0.5 {
		h = d / (2 - maxV - minV)
	} else {
		h = d / (maxV + minV)
	}
	s := h
	switch maxV {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return HSL{
		H: int(math.Round(h * 60)),
		S: int(math.Round(s * 100)),
		L: int(math.Round(l * 100)),
	}
}

func ToCMYK(c RGB) CMYK {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	k := 1 - math.Max(r, math.Max(g, b))
	if k >= 1 {
		return CMYK{K: 100}
	}
	return CMYK{
		C: int(math.Round((1 - r - k) / (1 - k) * 100)),
		M: int(math.Round((1 - g - k) / (1 - k) * 100)),
		Y: int(math.Round((1 - b - k) / (1 - k) * 100)),
		K: int(math.Round(k * 100)),
	}
}
