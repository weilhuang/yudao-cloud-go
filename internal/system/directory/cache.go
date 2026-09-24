package directory

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	javaCacheEvictTimeout = 5 * time.Second
	javaCacheScanCount    = 128
	javaCacheMaxScanPages = 4096
	javaCacheMaxKeys      = 100_000
)

// JavaRoleCache 是 Go 写入后同步失效 Java Spring Cache 的最小边界。
// 两端共享 MySQL/Redis 时，Java 会缓存角色、授权关系和部门子树。
type JavaRoleCache interface {
	EvictRole(ctx context.Context, tenantID, roleID int64) error
	EvictDeletedRole(ctx context.Context, tenantID, roleID int64) error
	EvictRoleMenus(ctx context.Context, tenantID int64) error
	EvictUserRoles(ctx context.Context, userID int64) error
	EvictDeptChildren(ctx context.Context, tenantID int64) error
	EvictMenuCreated(ctx context.Context, permission string) error
	EvictMenuChanged(ctx context.Context) error
	EvictMenusDeleted(ctx context.Context, menuIDs []int64) error
	// EvictNamedCache 清空一个 Java ignore-cache 的全部条目，名称如 mail_template。
	EvictNamedCache(ctx context.Context, name string) error
}

// JavaRoleRedisCache 只操作冻结版 Java RedisCacheManager 使用的键名。
// role/menu_role_ids 按租户分区；user_role_ids/permission_menu_ids 在 ignore-caches 中是全局缓存。
type JavaRoleRedisCache struct {
	Client *redis.Client
	prefix string
}

// NewJavaRoleRedisCache 的 rawPrefix 对应 Java spring.cache.redis.key-prefix。
// Java 只在前缀完全不含冒号时补一个冒号，这里保持相同规则。
func NewJavaRoleRedisCache(client *redis.Client, rawPrefix string) (*JavaRoleRedisCache, error) {
	if client == nil {
		return nil, fmt.Errorf("Java 角色缓存缺少 Redis 客户端")
	}
	// SCAN MATCH 是 Redis glob 语法；拒绝元字符，避免配置错误扩大扫描范围。
	if strings.ContainsAny(rawPrefix, "*?[]\\\r\n") {
		return nil, fmt.Errorf("Java 缓存前缀不能包含 Redis 通配符或换行")
	}
	if rawPrefix != "" && !strings.Contains(rawPrefix, ":") {
		rawPrefix += ":"
	}
	return &JavaRoleRedisCache{Client: client, prefix: rawPrefix}, nil
}

// EvictRole 删除本租户角色键，并清掉 Java 在忽略租户上下文时可能生成的同编号键。
// 两个键都是精确 DEL，不会删除其他租户的 role:<tenantId>:<roleId>。
func (c *JavaRoleRedisCache) EvictRole(ctx context.Context, tenantID, roleID int64) error {
	return c.Client.Del(ctx,
		c.prefix+"role:"+strconv.FormatInt(tenantID, 10)+":"+strconv.FormatInt(roleID, 10),
		c.prefix+"role:"+strconv.FormatInt(roleID, 10),
	).Err()
}

// EvictDeletedRole 对齐 Java deleteRole 与 processRoleDeleted 的组合失效。
func (c *JavaRoleRedisCache) EvictDeletedRole(ctx context.Context, tenantID, roleID int64) error {
	if err := c.EvictRole(ctx, tenantID, roleID); err != nil {
		return err
	}
	if err := c.scanDelete(ctx, c.prefix+"menu_role_ids:"+strconv.FormatInt(tenantID, 10)+":", decimalSuffix); err != nil {
		return err
	}
	return c.scanDelete(ctx, c.prefix+"user_role_ids:", decimalSuffix)
}

// EvictRoleMenus 对齐 Java assignRoleMenu 的两个 allEntries 缓存。
func (c *JavaRoleRedisCache) EvictRoleMenus(ctx context.Context, tenantID int64) error {
	if err := c.scanDelete(ctx, c.prefix+"menu_role_ids:"+strconv.FormatInt(tenantID, 10)+":", decimalSuffix); err != nil {
		return err
	}
	return c.scanDelete(ctx, c.prefix+"permission_menu_ids:", nonEmptySuffix)
}

// EvictUserRoles 对齐 Java assignUserRole 的精确 userId 键失效。
func (c *JavaRoleRedisCache) EvictUserRoles(ctx context.Context, userID int64) error {
	return c.Client.Del(ctx, c.prefix+"user_role_ids:"+strconv.FormatInt(userID, 10)).Err()
}

// EvictDeptChildren 对齐 Java DeptServiceImpl 的 allEntries 清理，始终限定当前租户。
func (c *JavaRoleRedisCache) EvictDeptChildren(ctx context.Context, tenantID int64) error {
	return c.scanDelete(ctx, c.prefix+"dept_children_ids:"+strconv.FormatInt(tenantID, 10)+":", decimalSuffix)
}

// EvictMenuCreated 对齐 Java 创建菜单时按权限标识精确失效。空权限不会产生可用键。
func (c *JavaRoleRedisCache) EvictMenuCreated(ctx context.Context, permission string) error {
	if permission == "" {
		return nil
	}
	return c.Client.Del(ctx, c.prefix+"permission_menu_ids:"+permission).Err()
}

// EvictMenuChanged 对齐 Java 修改菜单时清空全局 permission_menu_ids。
func (c *JavaRoleRedisCache) EvictMenuChanged(ctx context.Context) error {
	return c.scanDelete(ctx, c.prefix+"permission_menu_ids:", nonEmptySuffix)
}

// EvictNamedCache 对齐 @CacheEvict(allEntries=true)。键前缀与现有 Java 缓存一致。
func (c *JavaRoleRedisCache) EvictNamedCache(ctx context.Context, name string) error {
	if name == "" || strings.ContainsAny(name, "*: ") {
		return fmt.Errorf("缓存名称不合法")
	}
	return c.scanDelete(ctx, c.prefix+name+":", nonEmptySuffix)
}

// EvictMenusDeleted 清理被删菜单在每个租户的精确授权缓存及全局权限映射。
// 菜单和关系表是全局的，不能只清当前管理请求所在租户的菜单授权缓存。
func (c *JavaRoleRedisCache) EvictMenusDeleted(ctx context.Context, menuIDs []int64) error {
	wanted := make(map[int64]struct{}, len(menuIDs))
	for _, id := range menuIDs {
		wanted[id] = struct{}{}
	}
	if len(wanted) == 0 {
		return nil
	}
	if err := c.scanDelete(ctx, c.prefix+"menu_role_ids:", func(suffix string) bool {
		parts := strings.Split(suffix, ":")
		if len(parts) == 1 {
			// Java 忽略租户上下文时可能留下的精确菜单编号键。
			id, err := strconv.ParseInt(parts[0], 10, 64)
			_, ok := wanted[id]
			return err == nil && ok
		}
		if len(parts) != 2 || !decimalSuffix(parts[0]) || !decimalSuffix(parts[1]) {
			return false
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		_, ok := wanted[id]
		return err == nil && ok
	}); err != nil {
		return err
	}
	return c.EvictMenuChanged(ctx)
}

// scanDelete 使用小批量 SCAN + 精确前缀复核。页数和命中键数都有上限；
// 达到上限返回错误，不能把只清理了一部分键报告为成功。
func (c *JavaRoleRedisCache) scanDelete(ctx context.Context, prefix string, validSuffix func(string) bool) error {
	var cursor uint64
	matched := 0
	// 先完整扫描，再分批删除，避免边删边改变 Redis 哈希表导致当前游标漏键。
	found := make(map[string]struct{})
	for page := 0; page < javaCacheMaxScanPages; page++ {
		keys, next, err := c.Client.Scan(ctx, cursor, prefix+"*", javaCacheScanCount).Result()
		if err != nil {
			return fmt.Errorf("SCAN %q: %w", prefix, err)
		}
		matched += len(keys)
		if matched > javaCacheMaxKeys {
			return fmt.Errorf("SCAN %q 命中超过 %d 个键", prefix, javaCacheMaxKeys)
		}
		for _, key := range keys {
			if strings.HasPrefix(key, prefix) && validSuffix(strings.TrimPrefix(key, prefix)) {
				found[key] = struct{}{}
			}
		}
		cursor = next
		if cursor == 0 {
			batch := make([]string, 0, javaCacheScanCount)
			for key := range found {
				batch = append(batch, key)
				if len(batch) == javaCacheScanCount {
					if err := c.Client.Del(ctx, batch...).Err(); err != nil {
						return fmt.Errorf("DEL %q: %w", prefix, err)
					}
					batch = batch[:0]
				}
			}
			if len(batch) != 0 {
				if err := c.Client.Del(ctx, batch...).Err(); err != nil {
					return fmt.Errorf("DEL %q: %w", prefix, err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("SCAN %q 超过 %d 页", prefix, javaCacheMaxScanPages)
}

func decimalSuffix(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func nonEmptySuffix(value string) bool { return value != "" }

// evictAfterWrite 在 MySQL 已提交后，用独立短超时尝试失效 Redis。
// 客户端断开也要完成这次尝试；失败时明确告知调用方数据已提交，不能盲目重试写操作。
func (s *Service) evictAfterWrite(ctx context.Context, operation string, evict func(context.Context) error) error {
	if s.JavaRoleCache == nil {
		return nil
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), javaCacheEvictTimeout)
	defer cancel()
	if err := evict(cacheCtx); err != nil {
		slog.Error("Java 缓存失效失败，数据库已提交", "operation", operation, "err", err)
		return &Error{Code: 500, Msg: "数据库已提交，但 Java 缓存失效失败；请检查 Redis 并补偿清理缓存", Cause: fmt.Errorf("%s: %w", operation, err)}
	}
	return nil
}

func (s *Service) evictRoleAfterWrite(ctx context.Context, tenantID, roleID int64) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("角色更新 tenantId=%d roleId=%d", tenantID, roleID), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictRole(cacheCtx, tenantID, roleID)
	})
}

func (s *Service) evictDeletedRoleAfterWrite(ctx context.Context, tenantID, roleID int64) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("角色删除 tenantId=%d roleId=%d", tenantID, roleID), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictDeletedRole(cacheCtx, tenantID, roleID)
	})
}

func (s *Service) evictRoleMenusAfterWrite(ctx context.Context, tenantID int64) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("角色菜单授权 tenantId=%d", tenantID), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictRoleMenus(cacheCtx, tenantID)
	})
}

func (s *Service) evictUserRolesAfterWrite(ctx context.Context, userID int64) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("用户角色授权 userId=%d", userID), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictUserRoles(cacheCtx, userID)
	})
}

// EvictDeptChildrenAfterWrite 供部门写路径在数据库成功提交后调用。
func (s *Service) EvictDeptChildrenAfterWrite(ctx context.Context, tenantID int64) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("部门变更 tenantId=%d", tenantID), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictDeptChildren(cacheCtx, tenantID)
	})
}

func (s *Service) evictMenuCreatedAfterWrite(ctx context.Context, permission string) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("菜单创建 permission=%q", permission), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictMenuCreated(cacheCtx, permission)
	})
}

func (s *Service) evictMenuChangedAfterWrite(ctx context.Context) error {
	return s.evictAfterWrite(ctx, "菜单更新", func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictMenuChanged(cacheCtx)
	})
}

func (s *Service) evictMenusDeletedAfterWrite(ctx context.Context, ids []int64) error {
	return s.evictAfterWrite(ctx, fmt.Sprintf("菜单删除 menuIds=%v", ids), func(cacheCtx context.Context) error {
		return s.JavaRoleCache.EvictMenusDeleted(cacheCtx, ids)
	})
}
