package bootstrap

import (
	"context"
	"log"
	"time"

	"img2color/config"
	"img2color/internal/cache"
	"img2color/internal/service"
)

func NewService() *service.Service {
	cfg := config.Load()
	return service.New(cfg, buildCache(cfg))
}

func buildCache(cfg config.Config) cache.Store {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if cfg.UseRedisCache && cfg.RedisAddress != "" {
		return cache.NewRedisStore(cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
	}
	if cfg.UseMongoDB && cfg.MongoURI != "" {
		store, err := cache.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDB)
		if err != nil {
			log.Printf("mongodb cache disabled: %v", err)
			return cache.NoopStore{}
		}
		return store
	}
	return cache.NoopStore{}
}
