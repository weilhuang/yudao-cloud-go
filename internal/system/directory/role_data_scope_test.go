package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAssignRoleDataScopeValidatesRoleAndTenantDepartments(t *testing.T) {
	access := &memAccess{roles: map[int64]map[int64]*RoleSave{
		1: {7: {ID: 7, Type: roleTypeCustom}},
		2: {8: {ID: 8, Type: roleTypeCustom}, 9: {ID: 9, Type: roleTypeSystem}},
	}}
	svc := &Service{
		Access: access,
		Depts: &memDepts{ids: map[int64]map[int64]bool{
			1: {10: true, 11: true},
			2: {20: true},
		}},
	}
	assertErrorCode := func(name string, roleID int64, scope int, deptIDs []int64, code int) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			access.scope = 0
			err := svc.AssignRoleDataScope(context.Background(), 1, roleID, scope, deptIDs)
			biz, _ := err.(*Error)
			if biz == nil || biz.Code != code || access.scope != 0 {
				t.Fatalf("预期拒绝且不写入，实际 err=%v scope=%d", err, access.scope)
			}
		})
	}
	assertErrorCode("其他租户角色", 8, 2, []int64{10}, 1_002_002_000)
	assertErrorCode("角色不存在", 0, 2, []int64{10}, 1_002_002_000)
	assertErrorCode("数据范围枚举错误", 7, 6, []int64{10}, codeBadRequest)
	assertErrorCode("其他租户部门", 7, 2, []int64{20}, 1_002_004_002)
	assertErrorCode("不存在或已删除部门", 7, 2, []int64{99}, 1_002_004_002)

	if err := svc.AssignRoleDataScope(context.Background(), 2, 9, 2, []int64{20}); err == nil {
		t.Fatal("系统内置角色不能更改数据范围")
	} else if biz, ok := err.(*Error); !ok || biz.Code != 1_002_002_003 {
		t.Fatalf("系统内置角色应返回对应业务错误：%v", err)
	}
	if err := svc.AssignRoleDataScope(context.Background(), 1, 7, 2, []int64{10, 11, 10}); err != nil {
		t.Fatal(err)
	}
	if access.scope != 2 || len(access.scopeDeptIDs) != 2 || access.scopeDeptIDs[0] != 10 || access.scopeDeptIDs[1] != 11 {
		t.Fatalf("指定部门应去重并保持顺序：scope=%d depts=%v", access.scope, access.scopeDeptIDs)
	}
	// 切换到非自定义范围时，旧部门范围必须清空。
	if err := svc.AssignRoleDataScope(context.Background(), 1, 7, 5, []int64{20}); err != nil {
		t.Fatal(err)
	}
	if access.scope != 5 || len(access.scopeDeptIDs) != 0 {
		t.Fatalf("非自定义范围仍保留部门：scope=%d depts=%v", access.scope, access.scopeDeptIDs)
	}
}

func TestRoleGetResponseAndDataScopeRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access := &memAccess{role: &RoleSave{ID: 7, Type: roleTypeCustom}}
	h := &handler{svc: &Service{
		Reader: &memReader{role: &RoleDetail{ID: 7, DataScope: 2, DataScopeDeptIDs: []int64{10}}},
		Access: access,
		Depts:  &memDepts{ids: map[int64]map[int64]bool{1: {10: true}}},
	}}
	get := func(query string) map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/admin-api/system/role/get"+query, nil)
		h.roleGet(c, caller{tenantID: 1})
		var out map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	result := get("?id=7")
	var role RoleDetail
	if err := json.Unmarshal(result["data"], &role); err != nil || role.ID != 7 || len(role.DataScopeDeptIDs) != 1 || role.DataScopeDeptIDs[0] != 10 {
		t.Fatalf("角色详情未返回指定部门：%s, %v", result["data"], err)
	}
	if string(get("?id=bad")["code"]) != "400" {
		t.Fatal("非法角色编号应返回参数错误")
	}
	put := func(body string) map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin-api/system/permission/assign-role-data-scope", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.assignRoleDataScope(c, caller{tenantID: 1})
		var out map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if string(put(`{"roleId":7,"dataScope":2,"dataScopeDeptIds":[10]}`)["data"]) != "true" {
		t.Fatal("有效范围授权应返回 true")
	}
	if string(put(`{"roleId":7}`)["code"]) != "400" {
		t.Fatal("缺少 dataScope 应被拒绝")
	}
}
