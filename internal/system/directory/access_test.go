package directory

import (
	"context"
	"errors"
	"testing"
)

func TestSaveRoleRejectsSuperAdminCode(t *testing.T) {
	svc := &Service{Access: &memAccess{}}
	_, err := svc.SaveRole(context.Background(), 1, RoleSave{Name: "超管", Code: "super_admin"})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_002_005 {
		t.Fatal(err)
	}
}

func TestSaveRoleRejectsSystemRole(t *testing.T) {
	svc := &Service{Access: &memAccess{role: &RoleSave{ID: 1, Type: roleTypeSystem, Name: "管理员", Code: "admin"}}}
	_, err := svc.SaveRole(context.Background(), 1, RoleSave{ID: 1, Name: "管理员", Code: "admin"})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_002_003 {
		t.Fatal(err)
	}
}

func TestDeleteMenuRejectsChildren(t *testing.T) {
	svc := &Service{Access: &memAccess{menu: &MenuSave{ID: 1, Name: "系统", Type: menuDir}, children: 2}}
	err := svc.DeleteMenu(context.Background(), 1)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_001_004 {
		t.Fatal(err)
	}
}

func TestSaveMenuRejectsSelfParent(t *testing.T) {
	svc := &Service{Access: &memAccess{menu: &MenuSave{ID: 5, Name: "用户", Type: menuMenu}}}
	_, err := svc.SaveMenu(context.Background(), MenuSave{ID: 5, Name: "用户", ParentID: 5, Type: menuMenu})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_001_002 {
		t.Fatal(err)
	}
}

func TestAssignRoleMenusReplaces(t *testing.T) {
	access := &memAccess{role: &RoleSave{ID: 2, Type: roleTypeCustom, Name: "普通", Code: "common"}}
	svc := &Service{Access: access, Tenants: &memTenants{
		tenant: &Tenant{ID: 1, PackageID: 10},
		pkg:    &TenantPackage{ID: 10, MenuIDs: []int64{1, 3}},
	}}
	if err := svc.AssignRoleMenus(context.Background(), 1, 2, []int64{1, 2, 3, 3}); err != nil {
		t.Fatal(err)
	}
	if len(access.menus) != 2 || access.menus[0] != 1 || access.menus[1] != 3 {
		t.Fatalf("%v", access.menus)
	}
}

func TestAssignRoleMenusRespectsDifferentTenantPackages(t *testing.T) {
	for _, test := range []struct {
		name      string
		tenantID  int64
		packageID int64
		allowed   []int64
		want      int64
	}{
		{name: "租户一基础套餐", tenantID: 1, packageID: 10, allowed: []int64{101}, want: 101},
		{name: "租户二进阶套餐", tenantID: 2, packageID: 20, allowed: []int64{202}, want: 202},
	} {
		t.Run(test.name, func(t *testing.T) {
			access := &memAccess{role: &RoleSave{ID: 11}}
			svc := &Service{Access: access, Tenants: &memTenants{
				tenant: &Tenant{ID: test.tenantID, PackageID: test.packageID},
				pkg:    &TenantPackage{ID: test.packageID, MenuIDs: test.allowed},
			}}
			if err := svc.AssignRoleMenus(context.Background(), test.tenantID, 11, []int64{101, 202}); err != nil {
				t.Fatal(err)
			}
			if len(access.menus) != 1 || access.menus[0] != test.want {
				t.Fatalf("套餐外菜单未过滤：%v", access.menus)
			}
		})
	}
}

func TestAssignRoleMenusSystemTenantCanUseAllExistingMenus(t *testing.T) {
	access := &memAccess{
		role:      &RoleSave{ID: 33},
		menusByID: map[int64]*MenuSave{101: {ID: 101}, 202: {ID: 202}},
	}
	svc := &Service{Access: access, Tenants: &memTenants{tenant: &Tenant{ID: 3, PackageID: packageIDSystem}}}
	if err := svc.AssignRoleMenus(context.Background(), 3, 33, []int64{101, 202, 999}); err != nil {
		t.Fatal(err)
	}
	if len(access.menus) != 2 || access.menus[0] != 101 || access.menus[1] != 202 {
		t.Fatalf("系统租户应允许全部存在的菜单：%v", access.menus)
	}
}

func TestAssignRoleMenusMissingPackageKeepsOldGrant(t *testing.T) {
	access := &memAccess{role: &RoleSave{ID: 11}, menus: []int64{101}}
	svc := &Service{Access: access, Tenants: &memTenants{tenant: &Tenant{ID: 1, PackageID: 10}}}
	err := svc.AssignRoleMenus(context.Background(), 1, 11, []int64{202})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_016_000 || len(access.menus) != 1 || access.menus[0] != 101 {
		t.Fatalf("缺少套餐时不应改动授权：%v, %v", err, access.menus)
	}
}

func TestAssignUserRolesRejectsForeignTenantBeforeReplacing(t *testing.T) {
	access := &memAccess{roles: map[int64]map[int64]*RoleSave{
		1: {11: {ID: 11, Name: "本租户角色", Status: 0}},
		2: {22: {ID: 22, Name: "其他租户超管", Code: "super_admin", Status: 0}},
	}}
	svc := &Service{Reader: &memReader{user: &UserDetail{ID: 7}}, Access: access}
	err := svc.AssignUserRoles(context.Background(), 1, 7, []int64{11, 22})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_002_000 {
		t.Fatalf("跨租户角色应拒绝：%v", err)
	}
	if access.replaced {
		t.Fatal("校验失败后不应清空用户现有角色")
	}
	if err := svc.AssignUserRoles(context.Background(), 1, 7, []int64{11, 11}); err != nil {
		t.Fatal(err)
	}
	if !access.replaced || len(access.userRoles) != 1 || access.userRoles[0] != 11 {
		t.Fatalf("本租户角色应正常分配：%v", access.userRoles)
	}
}

func TestAssignUserRolesRejectsDisabledRole(t *testing.T) {
	access := &memAccess{roles: map[int64]map[int64]*RoleSave{
		1: {11: {ID: 11, Name: "停用角色", Status: 1}},
	}}
	svc := &Service{Reader: &memReader{user: &UserDetail{ID: 7}}, Access: access}
	err := svc.AssignUserRoles(context.Background(), 1, 7, []int64{11})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_002_004 || access.replaced {
		t.Fatalf("停用角色应拒绝且不写入：%v", err)
	}
}

type memAccess struct {
	role         *RoleSave
	roles        map[int64]map[int64]*RoleSave
	menu         *MenuSave
	menusByID    map[int64]*MenuSave
	children     int
	menus        []int64
	userRoles    []int64
	replaced     bool
	scope        int
	scopeDeptIDs []int64
	deletedRoles []int64
}

func (m *memAccess) RoleByID(_ context.Context, tenantID, roleID int64) (*RoleSave, error) {
	if m.roles != nil {
		return m.roles[tenantID][roleID], nil
	}
	return m.role, nil
}
func (m *memAccess) RoleNameTaken(context.Context, int64, string, int64) (bool, error) {
	return false, nil
}
func (m *memAccess) RoleCodeTaken(context.Context, int64, string, int64) (bool, error) {
	return false, nil
}
func (m *memAccess) CreateRole(context.Context, int64, RoleSave) (int64, error) { return 8, nil }
func (m *memAccess) UpdateRole(context.Context, int64, RoleSave) error          { return nil }
func (m *memAccess) UpdateRoleDataScope(_ context.Context, _, _ int64, scope int, deptIDs []int64) error {
	m.scope = scope
	m.scopeDeptIDs = append([]int64(nil), deptIDs...)
	return nil
}
func (m *memAccess) DeleteRole(context.Context, int64, int64) error { return nil }
func (m *memAccess) DeleteRoleList(_ context.Context, _ int64, ids []int64) error {
	m.deletedRoles = append(m.deletedRoles, ids...)
	return nil
}
func (m *memAccess) MenuByID(_ context.Context, menuID int64) (*MenuSave, error) {
	if m.menusByID != nil {
		return m.menusByID[menuID], nil
	}
	return m.menu, nil
}
func (m *memAccess) MenuNameTaken(context.Context, int64, string, int64) (bool, error) {
	return false, nil
}
func (m *memAccess) MenuComponentTaken(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (m *memAccess) MenuChildCount(context.Context, int64) (int, error)  { return m.children, nil }
func (m *memAccess) CreateMenu(context.Context, MenuSave) (int64, error) { return 1, nil }
func (m *memAccess) UpdateMenu(context.Context, MenuSave) error          { return nil }
func (m *memAccess) DeleteMenu(context.Context, int64) error             { return nil }
func (m *memAccess) DeleteMenuList(context.Context, []int64) error       { return nil }
func (m *memAccess) PostByID(context.Context, int64, int64) (*PostSave, error) {
	return nil, nil
}
func (m *memAccess) PostNameTaken(context.Context, int64, string, int64) (bool, error) {
	return false, nil
}
func (m *memAccess) PostCodeTaken(context.Context, int64, string, int64) (bool, error) {
	return false, nil
}
func (m *memAccess) CreatePost(context.Context, int64, PostSave) (int64, error) { return 1, nil }
func (m *memAccess) UpdatePost(context.Context, int64, PostSave) error          { return nil }
func (m *memAccess) DeletePost(context.Context, int64, int64) error             { return nil }
func (m *memAccess) DeletePostList(context.Context, int64, []int64) error       { return nil }
func (m *memAccess) RoleMenuIDs(context.Context, int64, int64) ([]int64, error) { return m.menus, nil }
func (m *memAccess) ReplaceRoleMenus(_ context.Context, _, _ int64, menuIDs []int64) error {
	m.menus = menuIDs
	return nil
}
func (m *memAccess) UserRoleIDs(context.Context, int64, int64) ([]int64, error) {
	return m.userRoles, nil
}
func (m *memAccess) ReplaceUserRoles(_ context.Context, _, _ int64, ids []int64) error {
	m.replaced = true
	m.userRoles = ids
	return nil
}

func TestDeleteRoleListEvictsAfterCommit(t *testing.T) {
	access := &memAccess{}
	cache := &javaRoleCacheSpy{}
	svc := &Service{Access: access, JavaRoleCache: cache}
	if err := svc.DeleteRoleList(context.Background(), 1, []int64{9, 4}); err != nil {
		t.Fatal(err)
	}
	if len(access.deletedRoles) != 2 || access.deletedRoles[0] != 9 || access.deletedRoles[1] != 4 {
		t.Fatalf("批删未交给存储：%v", access.deletedRoles)
	}
	if len(cache.calls) != 2 || cache.calls[0] != "deleted:1:9" || cache.calls[1] != "role:1:4" {
		t.Fatalf("缓存清理：%v", cache.calls)
	}
	cache.calls = nil
	access.deletedRoles = nil
	cache.err = errors.New("redis down")
	err := svc.DeleteRoleList(context.Background(), 1, []int64{4})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 500 || len(access.deletedRoles) != 1 {
		t.Fatalf("数据库已提交后缓存失败应明确报错：%v %v", err, access.deletedRoles)
	}
}
