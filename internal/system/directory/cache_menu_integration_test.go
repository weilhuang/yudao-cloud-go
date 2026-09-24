//go:build integration

package directory

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

// TestJavaMenuCacheRedis 验证全局菜单变更只清受影响的键，不误删其他菜单或角色缓存。
func TestJavaMenuCacheRedis(t *testing.T) {
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
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	cache, err := NewJavaRoleRedisCache(client, "mixed")
	if err != nil {
		t.Fatal(err)
	}
	seed := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			if err := client.Set(ctx, key, "cached", time.Hour).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(key string, want bool) {
		t.Helper()
		n, err := client.Exists(ctx, key).Result()
		if err != nil || (n == 1) != want {
			t.Fatalf("键 %q 存在=%v，期望 %v：%v", key, n == 1, want, err)
		}
	}

	seed("mixed:permission_menu_ids:system:a", "mixed:permission_menu_ids:system:b",
		"mixed:menu_role_ids:1:7", "mixed:menu_role_ids:2:7", "mixed:menu_role_ids:7",
		"mixed:menu_role_ids:1:8", "mixed:menu_role_ids:1:70", "mixed:role:2:7")
	if err := cache.EvictMenuCreated(ctx, "system:a"); err != nil {
		t.Fatal(err)
	}
	check("mixed:permission_menu_ids:system:a", false)
	check("mixed:permission_menu_ids:system:b", true)
	if err := cache.EvictMenuChanged(ctx); err != nil {
		t.Fatal(err)
	}
	check("mixed:permission_menu_ids:system:b", false)
	check("mixed:menu_role_ids:1:7", true)

	seed("mixed:permission_menu_ids:system:a", "mixed:permission_menu_ids:system:b",
		"mixed:menu_role_ids:bad:7", "mixed:menu_role_ids:1:7:bad")
	if err := cache.EvictMenusDeleted(ctx, []int64{7, 7}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mixed:menu_role_ids:1:7", "mixed:menu_role_ids:2:7",
		"mixed:menu_role_ids:7", "mixed:permission_menu_ids:system:a", "mixed:permission_menu_ids:system:b"} {
		check(key, false)
	}
	for _, key := range []string{"mixed:menu_role_ids:1:8", "mixed:menu_role_ids:1:70",
		"mixed:menu_role_ids:bad:7", "mixed:menu_role_ids:1:7:bad", "mixed:role:2:7"} {
		check(key, true)
	}
}
