//go:build integration

package redismonitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

// TestRedisMonitorWithRealRedis 以临时 Redis 验证 INFO 与选中逻辑库的 DBSIZE。
func TestRedisMonitorWithRealRedis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := rediscontainer.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	addr, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	selected := redis.NewClient(&redis.Options{Addr: addr, DB: 5})
	defer selected.Close()
	other := redis.NewClient(&redis.Options{Addr: addr, DB: 0})
	defer other.Close()
	if err := selected.Set(ctx, "monitor:one", "private-value", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := selected.Set(ctx, "monitor:two", "private-value", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := other.Set(ctx, "other:one", "private-value", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := selected.Get(ctx, "monitor:one").Err(); err != nil {
		t.Fatal(err)
	}
	got, err := Read(ctx, RedisReader{Client: selected})
	if err != nil {
		t.Fatal(err)
	}
	if got.DBSize != 2 || got.Info["redis_version"] == "" || got.Info["redis_mode"] != "standalone" || got.Info["used_memory_human"] == "" {
		t.Fatalf("Redis INFO/DBSIZE 与 Vben 契约不符: %+v", got)
	}
	var hasGet, hasSet bool
	for _, stat := range got.CommandStats {
		if stat.Command == "get" && stat.Calls > 0 {
			hasGet = true
		}
		if stat.Command == "set" && stat.Calls >= 3 {
			hasSet = true
		}
	}
	if !hasGet || !hasSet {
		t.Fatalf("真实 Redis 命令统计缺少 GET/SET: %+v", got.CommandStats)
	}
	for key, value := range got.Info {
		if sensitiveInfoKey(key) || strings.Contains(value, "private-value") || strings.Contains(value, "monitor:one") {
			t.Fatalf("监控结果泄露敏感字段或业务 Key: %s=%s", key, value)
		}
	}
}
