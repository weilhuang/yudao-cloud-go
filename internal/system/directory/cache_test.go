package directory

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

type javaRoleCacheSpy struct {
	calls []string
	err   error
}

type failingRoleUpdate struct {
	AccessStore
}

func (failingRoleUpdate) UpdateRole(context.Context, int64, RoleSave) error {
	return errors.New("MySQL 写入失败")
}

func (s *javaRoleCacheSpy) EvictRole(_ context.Context, tenantID, roleID int64) error {
	s.calls = append(s.calls, "role:"+intText(tenantID)+":"+intText(roleID))
	return s.err
}

func (s *javaRoleCacheSpy) EvictDeletedRole(_ context.Context, tenantID, roleID int64) error {
	s.calls = append(s.calls, "deleted:"+intText(tenantID)+":"+intText(roleID))
	return s.err
}

func (s *javaRoleCacheSpy) EvictRoleMenus(_ context.Context, tenantID int64) error {
	s.calls = append(s.calls, "menus:"+intText(tenantID))
	return s.err
}

func (s *javaRoleCacheSpy) EvictUserRoles(_ context.Context, userID int64) error {
	s.calls = append(s.calls, "user:"+intText(userID))
	return s.err
}

func (s *javaRoleCacheSpy) EvictNamedCache(_ context.Context, name string) error {
	s.calls = append(s.calls, "cache:"+name)
	return s.err
}

func (s *javaRoleCacheSpy) EvictDeptChildren(_ context.Context, tenantID int64) error {
	s.calls = append(s.calls, "dept:"+intText(tenantID))
	return s.err
}

func intText(n int64) string { return strconv.FormatInt(n, 10) }

func TestJavaRoleCacheFollowsCommittedWrites(t *testing.T) {
	ctx := context.Background()
	access := &memAccess{role: &RoleSave{ID: 7, Type: roleTypeCustom, Status: 0}}
	cache := &javaRoleCacheSpy{}
	svc := &Service{
		Access: access, Reader: &memReader{user: &UserDetail{ID: 8}},
		Tenants:       &memTenants{tenant: &Tenant{ID: 1, PackageID: 10}, pkg: &TenantPackage{ID: 10, MenuIDs: []int64{3}}},
		JavaRoleCache: cache,
	}
	if _, err := svc.SaveRole(ctx, 1, RoleSave{Name: "新角色", Code: "new"}); err != nil {
		t.Fatal(err)
	}
	if len(cache.calls) != 0 {
		t.Fatalf("新角色没有旧缓存，不应清理：%v", cache.calls)
	}
	if _, err := svc.SaveRole(ctx, 1, RoleSave{ID: 7, Name: "角色", Code: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignRoleDataScope(ctx, 1, 7, dataScopeAll, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignRoleMenus(ctx, 1, 7, []int64{3}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignUserRoles(ctx, 1, 8, []int64{7}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRole(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	if err := svc.EvictDeptChildrenAfterWrite(ctx, 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"role:1:7", "role:1:7", "menus:1", "user:8", "deleted:1:7", "dept:1"}
	if strings.Join(cache.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("缓存失效调用=%v，期望 %v", cache.calls, want)
	}
}

func TestJavaRoleCacheFailureReportsCommittedWrite(t *testing.T) {
	sentinel := errors.New("redis 不可用")
	cache := &javaRoleCacheSpy{err: sentinel}
	access := &memAccess{role: &RoleSave{ID: 7, Type: roleTypeCustom, Status: 0}}
	svc := &Service{Access: access, JavaRoleCache: cache}
	err := svc.AssignRoleDataScope(context.Background(), 1, 7, dataScopeAll, nil)
	biz, ok := err.(*Error)
	if !ok || biz.Code != 500 || !strings.Contains(biz.Msg, "数据库已提交") || !errors.Is(err, sentinel) {
		t.Fatalf("缓存故障未明确报告提交状态和根因：%v", err)
	}
	if access.scope != dataScopeAll {
		t.Fatalf("缓存失效发生在数据库写入前：scope=%d", access.scope)
	}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer client.Close()
	if _, err := NewJavaRoleRedisCache(client, "unsafe*"); err == nil {
		t.Fatal("含 Redis 通配符的前缀应在启动时拒绝")
	}
	withColon, err := NewJavaRoleRedisCache(client, "app:cache")
	if err != nil {
		t.Fatal(err)
	}
	if withColon.prefix != "app:cache" {
		t.Fatalf("Java 前缀已含冒号时不得再追加：prefix=%q", withColon.prefix)
	}
}

func TestJavaRoleCacheSkipsFailedWrite(t *testing.T) {
	cache := &javaRoleCacheSpy{}
	access := &memAccess{role: &RoleSave{ID: 7, Type: roleTypeCustom}}
	svc := &Service{Access: failingRoleUpdate{AccessStore: access}, JavaRoleCache: cache}
	if _, err := svc.SaveRole(context.Background(), 1, RoleSave{ID: 7, Name: "角色", Code: "test"}); err == nil {
		t.Fatal("MySQL 写入失败应向上返回")
	}
	if len(cache.calls) != 0 {
		t.Fatalf("数据库未提交却清了 Java 缓存：%v", cache.calls)
	}
}

func TestSaveDeptEvictsCurrentTenantChildrenAfterWrite(t *testing.T) {
	cache := &javaRoleCacheSpy{}
	svc := &Service{Depts: memDepts{ids: map[int64]map[int64]bool{2: {5: true}}}, JavaRoleCache: cache}
	if id, err := svc.SaveDept(context.Background(), 1, Dept{Name: "新部门"}); err != nil || id != 1 {
		t.Fatalf("新建部门失败：id=%d err=%v", id, err)
	}
	if id, err := svc.SaveDept(context.Background(), 2, Dept{ID: 5, Name: "更新部门"}); err != nil || id != 5 {
		t.Fatalf("更新部门失败：id=%d err=%v", id, err)
	}
	if got := strings.Join(cache.calls, ","); got != "dept:1,dept:2" {
		t.Fatalf("部门缓存未按所属租户失效：%s", got)
	}
	cache.err = errors.New("Redis 不可用")
	if id, err := svc.SaveDept(context.Background(), 1, Dept{Name: "已提交"}); id != 1 || err == nil {
		t.Fatalf("写入后 Redis 故障应返回已有编号与错误：id=%d err=%v", id, err)
	}
}
