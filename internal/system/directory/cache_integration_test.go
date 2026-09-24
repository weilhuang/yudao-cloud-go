//go:build integration

package directory

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

// TestJavaRoleCacheRedis 使用独立 Redis 验证 Java 键格式与租户隔离。
func TestJavaRoleCacheRedis(t *testing.T) {
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
			if err := client.Set(ctx, key, "java-value", time.Hour).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertKeys := func(gone, kept []string) {
		t.Helper()
		for _, key := range gone {
			if exists, err := client.Exists(ctx, key).Result(); err != nil || exists != 0 {
				t.Fatalf("键 %q 应已失效：exists=%d err=%v", key, exists, err)
			}
		}
		for _, key := range kept {
			if exists, err := client.Exists(ctx, key).Result(); err != nil || exists != 1 {
				t.Fatalf("键 %q 应保留：exists=%d err=%v", key, exists, err)
			}
		}
	}

	seed("mixed:role:1:7", "mixed:role:7", "mixed:role:2:7", "mixed:role:1:8", "role:1:7")
	if err := cache.EvictRole(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	assertKeys([]string{"mixed:role:1:7", "mixed:role:7"},
		[]string{"mixed:role:2:7", "mixed:role:1:8", "role:1:7"})

	seed("mixed:menu_role_ids:1:3", "mixed:menu_role_ids:1:4", "mixed:menu_role_ids:2:3",
		"mixed:menu_role_ids:10:3", "mixed:menu_role_ids:1:bad", "mixed:permission_menu_ids:system:user:query",
		"mixed:permission_menu_ids:system:role:query", "mixed:permission_menu_ids:")
	if err := cache.EvictRoleMenus(ctx, 1); err != nil {
		t.Fatal(err)
	}
	assertKeys([]string{"mixed:menu_role_ids:1:3", "mixed:menu_role_ids:1:4",
		"mixed:permission_menu_ids:system:user:query", "mixed:permission_menu_ids:system:role:query"},
		[]string{"mixed:menu_role_ids:2:3", "mixed:menu_role_ids:10:3", "mixed:menu_role_ids:1:bad",
			"mixed:permission_menu_ids:"})

	seed("mixed:role:1:9", "mixed:role:2:9", "mixed:menu_role_ids:1:5",
		"mixed:menu_role_ids:2:5", "mixed:user_role_ids:8", "mixed:user_role_ids:2:8")
	if err := cache.EvictDeletedRole(ctx, 1, 9); err != nil {
		t.Fatal(err)
	}
	assertKeys([]string{"mixed:role:1:9", "mixed:menu_role_ids:1:5", "mixed:user_role_ids:8"},
		[]string{"mixed:role:2:9", "mixed:menu_role_ids:2:5", "mixed:user_role_ids:2:8"})

	seed("mixed:user_role_ids:10", "mixed:user_role_ids:11")
	if err := cache.EvictUserRoles(ctx, 10); err != nil {
		t.Fatal(err)
	}
	assertKeys([]string{"mixed:user_role_ids:10"}, []string{"mixed:user_role_ids:11"})

	seed("mixed:dept_children_ids:1:0", "mixed:dept_children_ids:1:3",
		"mixed:dept_children_ids:2:3", "mixed:dept_children_ids:10:3")
	if err := cache.EvictDeptChildren(ctx, 1); err != nil {
		t.Fatal(err)
	}
	assertKeys([]string{"mixed:dept_children_ids:1:0", "mixed:dept_children_ids:1:3"},
		[]string{"mixed:dept_children_ids:2:3", "mixed:dept_children_ids:10:3"})

	seed("mixed:mail_template::1", "mixed:mail_template:2", "mixed:sms_template::9", "mixed:oauth_client::1")
	if err := cache.EvictNamedCache(ctx, "mail_template"); err != nil {
		t.Fatal(err)
	}
	assertKeys([]string{"mixed:mail_template::1", "mixed:mail_template:2"},
		[]string{"mixed:sms_template::9", "mixed:oauth_client::1"})

	// 模拟提交后 Redis 客户端断开；业务错误必须说明写入已经提交。
	closedClient := redis.NewClient(&redis.Options{Addr: addr})
	if err := closedClient.Close(); err != nil {
		t.Fatal(err)
	}
	closedCache, err := NewJavaRoleRedisCache(closedClient, "mixed")
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Access: &memAccess{role: &RoleSave{ID: 7, Type: roleTypeCustom}}, JavaRoleCache: closedCache}
	id, err := svc.SaveRole(ctx, 1, RoleSave{ID: 7, Name: "已提交", Code: "committed"})
	biz, ok := err.(*Error)
	if id != 7 || !ok || biz.Code != 500 {
		t.Fatalf("Redis 断开后应明确返回已提交错误：id=%d err=%v", id, err)
	}
}
