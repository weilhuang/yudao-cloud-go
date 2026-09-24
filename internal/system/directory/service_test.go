package directory

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestCreateUserRejectsDuplicate(t *testing.T) {
	svc := &Service{Writer: &memWriter{taken: true}, Reader: &memReader{}, BcryptCost: 4}
	_, err := svc.CreateUser(context.Background(), 1, UserSave{Username: "admin", Nickname: "芋道", Password: "admin123"})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != codeUsernameExists {
		t.Fatal(err)
	}
}

func TestCreateUserStoresBcrypt(t *testing.T) {
	writer := &memWriter{}
	svc := &Service{Writer: writer, Reader: &memReader{}, BcryptCost: 4}
	id, err := svc.CreateUser(context.Background(), 1, UserSave{Username: "neo", Nickname: "尼奥", Password: "admin123"})
	if err != nil || id != 9 {
		t.Fatal(err, id)
	}
	if bcrypt.CompareHashAndPassword([]byte(writer.hash), []byte("admin123")) != nil {
		t.Fatal("密码哈希无法校验")
	}
}

func TestNewPasswordUsesDefaultBcryptCost(t *testing.T) {
	writer := &memWriter{}
	svc := &Service{Writer: writer, Reader: &memReader{}}
	if _, err := svc.CreateUser(context.Background(), 1, UserSave{Username: "neo", Nickname: "尼奥", Password: "admin123"}); err != nil {
		t.Fatal(err)
	}
	cost, err := bcrypt.Cost([]byte(writer.hash))
	if err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("新密码应使用 bcrypt 默认成本 %d，实际 %d: %v", bcrypt.DefaultCost, cost, err)
	}
}

func TestSaveDeptRequiresParent(t *testing.T) {
	svc := &Service{Depts: &memDepts{}}
	_, err := svc.SaveDept(context.Background(), 1, Dept{Name: "小组", ParentID: 99})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != codeDeptParentNotFound {
		t.Fatal(err)
	}
}

func TestSaveDeptRejectsSelfAndDescendant(t *testing.T) {
	writer := &deptWriteSpy{memDepts: memDepts{ids: map[int64]map[int64]bool{
		1: {1: true, 2: true, 3: true},
		2: {4: true},
	}}}
	reader := &deptParentReader{depts: map[int64]map[int64]Dept{
		1: {
			1: {ID: 1, ParentID: 0},
			2: {ID: 2, ParentID: 1},
			3: {ID: 3, ParentID: 2},
		},
		2: {4: {ID: 4, ParentID: 0}},
	}}
	svc := &Service{Reader: reader, Depts: writer}
	for _, tt := range []struct {
		name     string
		id       int64
		parentID int64
		want     int
	}{
		{"自己作为父部门", 1, 1, codeDeptParentSelf},
		{"直接子部门作为父部门", 1, 2, codeDeptParentIsChild},
		{"孙部门作为父部门", 1, 3, codeDeptParentIsChild},
		{"跨租户父部门", 1, 4, codeDeptParentNotFound},
		{"更新不存在的部门", 99, 0, codeDeptNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.SaveDept(context.Background(), 1, Dept{ID: tt.id, Name: "总部", ParentID: tt.parentID})
			biz, _ := err.(*Error)
			if biz == nil || biz.Code != tt.want {
				t.Fatalf("错误码不符：got=%v want=%d", err, tt.want)
			}
			if len(writer.updated) != 0 {
				t.Fatalf("非法层级不能调用写存储：%v", writer.updated)
			}
		})
	}
	if _, err := svc.SaveDept(context.Background(), 1, Dept{ID: 3, Name: "设计", ParentID: 1}); err != nil {
		t.Fatalf("合法改父失败：%v", err)
	}
	if len(writer.updated) != 1 || writer.updated[0].ID != 3 || writer.updated[0].ParentID != 1 {
		t.Fatalf("合法改父未写入：%v", writer.updated)
	}
}

func TestDeleteSelfMatchesJava(t *testing.T) {
	writer := &memWriter{}
	cache := &javaRoleCacheSpy{}
	svc := &Service{Writer: writer, Reader: &memReader{user: &UserDetail{ID: 3}}, JavaRoleCache: cache}
	if err := svc.DeleteUser(context.Background(), 1, 3, 3); err != nil {
		t.Fatal(err)
	}
	if len(writer.deleted) != 1 || writer.deleted[0] != 3 {
		t.Fatalf("应删除当前用户：%v", writer.deleted)
	}
	if len(cache.calls) != 1 || cache.calls[0] != "user:3" {
		t.Fatalf("应失效该用户角色缓存：%v", cache.calls)
	}
}

func TestDeleteMissingUserSkipsCache(t *testing.T) {
	cache := &javaRoleCacheSpy{}
	svc := &Service{Writer: &memWriter{}, Reader: &memReader{}, JavaRoleCache: cache}
	err := svc.DeleteUser(context.Background(), 1, 7, 9)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != codeUserNotExists || len(cache.calls) != 0 {
		t.Fatal(err, cache.calls)
	}
}

func TestDisableUserRevokesAdminTokens(t *testing.T) {
	var revoked []int64
	svc := &Service{
		Writer: &memWriter{}, Reader: &memReader{user: &UserDetail{ID: 4}},
		RevokeAdminTokens: func(_ context.Context, tenantID, userID int64) error {
			if tenantID != 1 {
				t.Fatalf("租户 %d", tenantID)
			}
			revoked = append(revoked, userID)
			return nil
		},
	}
	if err := svc.UpdateStatus(context.Background(), 1, 4, 1); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 || revoked[0] != 4 {
		t.Fatalf("禁用应撤销令牌：%v", revoked)
	}
	if err := svc.UpdateStatus(context.Background(), 1, 4, 0); err != nil {
		t.Fatal(err)
	}
	if len(revoked) != 1 {
		t.Fatal("启用不应再次撤销令牌")
	}
	svc.RevokeAdminTokens = func(context.Context, int64, int64) error { return errors.New("redis down") }
	err := svc.UpdateStatus(context.Background(), 1, 4, 1)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 500 || biz.Msg != "数据库已提交，但访问令牌撤销失败；请检查后补偿清理令牌" {
		t.Fatal(err)
	}
}

type memWriter struct {
	taken   bool
	hash    string
	deleted []int64
}

func (m *memWriter) UsernameTaken(context.Context, int64, string, int64) (bool, error) {
	return m.taken, nil
}
func (m *memWriter) CreateUser(_ context.Context, _ int64, _ UserSave, hash string) (int64, error) {
	m.hash = hash
	return 9, nil
}
func (m *memWriter) UpdateUser(context.Context, int64, UserSave) error { return nil }
func (m *memWriter) UpdatePassword(_ context.Context, _, _ int64, hash string) error {
	m.hash = hash
	return nil
}
func (m *memWriter) UpdateStatus(context.Context, int64, int64, int) error { return nil }
func (m *memWriter) DeleteUser(_ context.Context, _, id int64) error {
	m.deleted = append(m.deleted, id)
	return nil
}
func (m *memWriter) DeleteUserList(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	m.deleted = append(m.deleted, ids...)
	return append([]int64(nil), ids...), nil
}
func (m *memWriter) ImportUsers(context.Context, int64, []ImportUser, bool, func(string) (string, error)) (ImportResult, error) {
	return ImportResult{}, nil
}

type memDepts struct{ ids map[int64]map[int64]bool }

func (m memDepts) DeptExists(_ context.Context, tenantID, id int64) (bool, error) {
	return m.ids[tenantID][id], nil
}
func (memDepts) CreateDept(context.Context, int64, Dept) (int64, error) { return 1, nil }
func (memDepts) UpdateDept(context.Context, int64, Dept) error          { return nil }
func (memDepts) DeleteDept(context.Context, int64, int64) error         { return nil }
func (memDepts) DeleteDeptList(context.Context, int64, []int64) error   { return nil }

type deptWriteSpy struct {
	memDepts
	updated []Dept
}

func (m *deptWriteSpy) UpdateDept(_ context.Context, _ int64, dept Dept) error {
	m.updated = append(m.updated, dept)
	return nil
}

type deptParentReader struct {
	memReader
	depts map[int64]map[int64]Dept
}

func (r *deptParentReader) DeptGet(_ context.Context, tenantID, id int64) (*Dept, error) {
	dept, ok := r.depts[tenantID][id]
	if !ok {
		return nil, nil
	}
	return &dept, nil
}

type memReader struct {
	user *UserDetail
	role *RoleDetail
}

func (memReader) DictSimple(context.Context) ([]DictItem, error) { return nil, nil }
func (memReader) DictEnabledByType(context.Context, string) ([]DictData, error) {
	return nil, nil
}
func (memReader) DeptList(context.Context, int64) ([]Dept, error)         { return nil, nil }
func (memReader) DeptGet(context.Context, int64, int64) (*Dept, error)    { return nil, nil }
func (memReader) UserSimple(context.Context, int64) ([]UserSimple, error) { return nil, nil }
func (memReader) UserPage(context.Context, int64, UserQuery) (Page[UserDetail], error) {
	return Page[UserDetail]{}, nil
}
func (m memReader) UserGet(context.Context, int64, int64) (*UserDetail, error) { return m.user, nil }
func (memReader) MenuSimple(context.Context) ([]MenuSimple, error)             { return nil, nil }
func (memReader) RoleSimple(context.Context, int64) ([]RoleSimple, error)      { return nil, nil }
func (m memReader) RoleGet(context.Context, int64, int64) (*RoleDetail, error) { return m.role, nil }
func (memReader) MenuList(context.Context, string, *int) ([]MenuDetail, error) {
	return nil, nil
}
func (memReader) RolePage(context.Context, int64, int, int, string, string, *int) (Page[RoleDetail], error) {
	return Page[RoleDetail]{}, nil
}
func (memReader) PostSimple(context.Context, int64) ([]PostSimple, error)    { return nil, nil }
func (memReader) PostGet(context.Context, int64, int64) (*PostDetail, error) { return nil, nil }
func (memReader) PostPage(context.Context, int64, PostQuery) (Page[PostDetail], error) {
	return Page[PostDetail]{}, nil
}
func (memReader) PostExportRows(context.Context, int64, PostQuery, func(PostDetail) error) error {
	return nil
}
func (memReader) PostStatusLabels(context.Context) (map[int]string, error) { return nil, nil }
func (memReader) PostsByIDs(context.Context, int64, []int64) ([]PostSimple, error) {
	return nil, nil
}
func (memReader) UserListByIDs(context.Context, int64, []int64) ([]UserDetail, error) {
	return nil, nil
}
func (memReader) UserByNickname(context.Context, int64, string) ([]UserSimple, error) {
	return nil, nil
}
func (memReader) UserExportRows(context.Context, int64, UserQuery, func(UserDetail) error) error {
	return nil
}
func (memReader) RoleExportRows(context.Context, int64, string, string, *int, func(RoleDetail) error) error {
	return nil
}
func (memReader) DictLabels(context.Context, string) (map[int]string, error) { return nil, nil }
