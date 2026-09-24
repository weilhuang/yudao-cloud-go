package captcha

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 把滑块坐标放进 Redis，多实例登录时也能校验。
type Redis struct {
	Client *redis.Client
}

func (r Redis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.Client.Set(ctx, key, value, ttl).Err()
}

func (r Redis) GetDel(ctx context.Context, key string) (string, error) {
	value, err := r.Client.GetDel(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return value, err
}
