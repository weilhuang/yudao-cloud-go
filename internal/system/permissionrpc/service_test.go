package permissionrpc

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeReader struct {
	userIDs     []int64
	roles       []Role
	permissions map[string]bool
	dept        *int64
	children    map[int64][]int64
	err         error
	rolesRead   int
	deptRead    int
}

func (f *fakeReader) UserIDsByRoleIDs(_ context.Context, _ int64, _ []int64) ([]int64, error) {
	return f.userIDs, f.err
}
func (f *fakeReader) EnabledRolesByUser(_ context.Context, _ int64, _ int64) ([]Role, error) {
	f.rolesRead++
	return f.roles, f.err
}
func (f *fakeReader) RolesHavePermission(_ context.Context, _ int64, _ []int64, permission string) (bool, error) {
	return f.permissions[permission], f.err
}
func (f *fakeReader) UserDeptID(_ context.Context, _ int64, _ int64) (*int64, error) {
	f.deptRead++
	return f.dept, f.err
}
func (f *fakeReader) ChildDeptIDs(_ context.Context, _ int64, deptID int64) ([]int64, error) {
	return f.children[deptID], f.err
}

func TestUserIDsByRoles(t *testing.T) {
	reader := &fakeReader{userIDs: []int64{3, 1, 3, 2}}
	svc := &Service{Reader: reader}
	got, err := svc.UserIDsByRoleIDs(context.Background(), 1, []int64{8, 9})
	if err != nil || !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("去重排序：%v, %v", got, err)
	}
	got, err = svc.UserIDsByRoleIDs(context.Background(), 1, nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("空角色应返回空数组：%v, %v", got, err)
	}
}

func TestPermissionAndRoleRules(t *testing.T) {
	reader := &fakeReader{roles: []Role{{ID: 1, Code: "editor"}}, permissions: map[string]bool{"write": true}}
	svc := &Service{Reader: reader}
	checkPermission := func(input []string, want bool) {
		t.Helper()
		got, err := svc.HasAnyPermissions(context.Background(), 1, 4, input)
		if err != nil || got != want {
			t.Fatalf("permissions=%v: got=%v err=%v want=%v", input, got, err, want)
		}
	}
	checkPermission(nil, true)
	if reader.rolesRead != 0 {
		t.Fatal("空权限不应访问数据库")
	}
	checkPermission([]string{"read", "write"}, true)
	checkPermission([]string{"unknown"}, false)
	reader.roles = []Role{{ID: 2, Code: superAdminCode}}
	checkPermission([]string{"unknown"}, true)
	reader.roles = nil
	checkPermission([]string{"unknown"}, false)
	got, err := svc.HasAnyRoles(context.Background(), 1, 4, nil)
	if err != nil || !got {
		t.Fatalf("空角色数组应放行：%v, %v", got, err)
	}
	reader.roles = []Role{{ID: 1, Code: "editor"}}
	got, err = svc.HasAnyRoles(context.Background(), 1, 4, []string{"viewer", "editor"})
	if err != nil || !got {
		t.Fatalf("任意角色匹配失败：%v, %v", got, err)
	}
	got, err = svc.HasAnyRoles(context.Background(), 1, 4, []string{"super_admin"})
	if err != nil || got {
		t.Fatalf("不存在的角色误放行：%v, %v", got, err)
	}
}

func TestDeptDataPermissionUnion(t *testing.T) {
	deptID := int64(10)
	reader := &fakeReader{dept: &deptID, children: map[int64][]int64{10: {11, 12}}, roles: []Role{
		{ID: 1, DataScope: scopeAll},
		{ID: 2, DataScope: scopeDeptCustom, ScopeDeptIDs: []int64{9, 11}},
		{ID: 3, DataScope: scopeDeptOnly},
		{ID: 4, DataScope: scopeDeptAndChild},
		{ID: 5, DataScope: scopeSelf},
	}}
	svc := &Service{Reader: reader}
	got, err := svc.GetDeptDataPermission(context.Background(), 1, 4)
	if err != nil || !got.All || !got.Self || !reflect.DeepEqual(got.DeptIDs, []int64{9, 10, 11, 12}) {
		t.Fatalf("多角色部门范围合并失败：%+v, %v", got, err)
	}
	if reader.deptRead != 1 {
		t.Fatalf("用户部门应惰性查询一次，实际 %d", reader.deptRead)
	}
	reader.roles = nil
	got, err = svc.GetDeptDataPermission(context.Background(), 1, 4)
	if err != nil || got.All || !got.Self || got.DeptIDs == nil || len(got.DeptIDs) != 0 {
		t.Fatalf("无角色只能看本人：%+v, %v", got, err)
	}
	reader.roles = []Role{{ID: 6, DataScope: scopeDeptAndChild}}
	reader.dept = nil
	got, err = svc.GetDeptDataPermission(context.Background(), 1, 4)
	if err != nil || got.All || got.Self || len(got.DeptIDs) != 0 {
		t.Fatalf("无部门时不应查后代：%+v, %v", got, err)
	}
}

func TestReadErrorIsNotConvertedToPermissionGrant(t *testing.T) {
	reader := &fakeReader{err: errors.New("database unavailable")}
	svc := &Service{Reader: reader}
	if ok, err := svc.HasAnyPermissions(context.Background(), 1, 4, []string{"read"}); err == nil || ok {
		t.Fatalf("权限查询故障不应放行：ok=%v err=%v", ok, err)
	}
	if ok, err := svc.HasAnyRoles(context.Background(), 1, 4, []string{"editor"}); err == nil || ok {
		t.Fatalf("角色查询故障不应放行：ok=%v err=%v", ok, err)
	}
}
