package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

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

func Key(rawURL, format string, colors, maxWidth int, skipBackground, kmeans bool) string {
	sum := md5.Sum([]byte(rawURL))
	return hex.EncodeToString(sum[:]) + ":" + format + ":" + itoa(colors) + ":" + itoa(maxWidth) + ":" + boolStr(skipBackground) + ":" + boolStr(kmeans)
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
