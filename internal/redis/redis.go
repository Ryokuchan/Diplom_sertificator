package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

func Connect(url string) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:         url,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
		MinIdleConns: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		// Не паникуем — Redis опционален для кэша, логируем при старте
		_ = err
	}

	return client
}
