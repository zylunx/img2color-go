package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

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

func (r *RedisStore) Close(ctx context.Context) error {
	return r.client.Close()
}
