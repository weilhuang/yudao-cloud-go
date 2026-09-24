package directory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func (s *javaRoleCacheSpy) EvictMenuCreated(_ context.Context, permission string) error {
	s.calls = append(s.calls, "menu-created:"+permission)
	return s.err
}

func (s *javaRoleCacheSpy) EvictMenuChanged(context.Context) error {
	s.calls = append(s.calls, "menu-changed")
	return s.err
}

func (s *javaRoleCacheSpy) EvictMenusDeleted(_ context.Context, ids []int64) error {
	s.calls = append(s.calls, "menu-deleted:"+strings.ReplaceAll(strings.Trim(fmt.Sprint(ids), "[]"), " ", ","))
	return s.err
}

type failingMenuAccess struct{ AccessStore }

func (failingMenuAccess) UpdateMenu(context.Context, MenuSave) error {
	return errors.New("MySQL 菜单更新失败")
}

type failingTenantUpdate struct{ TenantStore }

func (failingTenantUpdate) UpdateTenant(context.Context, Tenant, []int64, bool) error {
	return errors.New("MySQL 租户更新失败")
}

func TestMenuAndTenantCacheEvictionAfterWrite(t *testing.T) {
	ctx := context.Background()
	cache := &javaRoleCacheSpy{}
	access := &memAccess{menu: &MenuSave{ID: 1, Name: "菜单", Type: menuMenu}}
	tenant := &memTenants{
		tenant: &Tenant{ID: 8, PackageID: 10},
		pkg:    &TenantPackage{ID: 11, Name: "新套餐", MenuIDs: []int64{2}},
	}
	svc := &Service{Access: access, Tenants: tenant, JavaRoleCache: cache}
	if _, err := svc.SaveMenu(ctx, MenuSave{Name: "新增", Permission: "system:new:view"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveMenu(ctx, MenuSave{ID: 1, Name: "修改"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMenu(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMenuList(ctx, []int64{2, 3}); err != nil {
		t.Fatal(err)
	}
	updated := sampleTenant(8)
	updated.PackageID = 11
	if _, err := svc.SaveTenant(ctx, updated); err != nil {
		t.Fatal(err)
	}
	want := []string{"menu-created:system:new:view", "menu-changed", "menu-deleted:1", "menu-deleted:2,3", "menus:8"}
	if !reflect.DeepEqual(cache.calls, want) || !tenant.synced {
		t.Fatalf("提交后缓存失效调用=%v，套餐同步=%v；期望 %v", cache.calls, tenant.synced, want)
	}
}

func TestMenuAndTenantCacheNotEvictedOnFailedWrite(t *testing.T) {
	ctx := context.Background()
	cache := &javaRoleCacheSpy{}
	access := &memAccess{menu: &MenuSave{ID: 1, Name: "菜单", Type: menuMenu}}
	svc := &Service{Access: failingMenuAccess{AccessStore: access}, JavaRoleCache: cache}
	if _, err := svc.SaveMenu(ctx, MenuSave{ID: 1, Name: "修改"}); err == nil {
		t.Fatal("菜单数据库写入失败必须报错")
	}
	tenant := &memTenants{tenant: &Tenant{ID: 8, PackageID: 10}, pkg: &TenantPackage{ID: 11, Name: "新套餐"}}
	svc.Tenants = failingTenantUpdate{TenantStore: tenant}
	updated := sampleTenant(8)
	updated.PackageID = 11
	if _, err := svc.SaveTenant(ctx, updated); err == nil {
		t.Fatal("租户数据库写入失败必须报错")
	}
	if len(cache.calls) != 0 {
		t.Fatalf("未提交时不应失效缓存：%v", cache.calls)
	}
}

func TestMenuCacheFailureReportsCommittedWrite(t *testing.T) {
	cache := &javaRoleCacheSpy{err: errors.New("Redis 不可用")}
	svc := &Service{Access: &memAccess{}, JavaRoleCache: cache}
	id, err := svc.SaveMenu(context.Background(), MenuSave{Name: "已创建", Permission: "system:new:view"})
	biz, ok := err.(*Error)
	if id != 1 || !ok || biz.Code != 500 || !strings.Contains(biz.Msg, "数据库已提交") {
		t.Fatalf("菜单已写库后 Redis 故障未明确返回：id=%d err=%v", id, err)
	}
}
