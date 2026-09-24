package permissionrpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/system/directory"
)

func TestPermissionRPCRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	reader := &fakeReader{userIDs: []int64{7, 7, 8}, roles: []Role{{ID: 3, Code: "editor", DataScope: scopeSelf}},
		permissions: map[string]bool{"edit": true}}
	var seenTenant int64
	MountRPC(r, &Service{Reader: reader}, func(_ context.Context, tenantID int64, _ time.Time) error {
		seenTenant = tenantID
		if tenantID == 2 {
			return &directory.Error{Code: 1_002_015_001, Msg: "租户已禁用"}
		}
		return nil
	})
	call := func(path, tenant string) struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	} {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if tenant != "" {
			req.Header.Set("tenant-id", tenant)
		}
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s 返回 HTTP %d: %s", path, w.Code, w.Body.String())
		}
		var result struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if res := call("/rpc-api/system/permission/user-role-id-list-by-role-id?roleIds=3,4&roleIds=5", "1"); res.Code != 0 || string(res.Data) != "[7,8]" || seenTenant != 1 {
		t.Fatalf("角色用户集合不符：%+v tenant=%d", res, seenTenant)
	}
	if res := call("/rpc-api/system/permission/has-any-permissions?userId=8&permissions=unknown,edit", "1"); res.Code != 0 || string(res.Data) != "true" {
		t.Fatalf("权限判断不符：%+v", res)
	}
	if res := call("/rpc-api/system/permission/has-any-roles?userId=8&roles=viewer&roles=editor", "1"); res.Code != 0 || string(res.Data) != "true" {
		t.Fatalf("角色判断不符：%+v", res)
	}
	if res := call("/rpc-api/system/permission/get-dept-data-permission?userId=8", "1"); res.Code != 0 || string(res.Data) != `{"all":false,"self":true,"deptIds":[]}` {
		t.Fatalf("部门范围不符：%+v", res)
	}
	for _, path := range []string{
		"/rpc-api/system/permission/has-any-permissions?userId=8&permissions=edit",
		"/rpc-api/system/permission/has-any-roles?userId=8&roles=editor",
		"/rpc-api/system/permission/get-dept-data-permission?userId=8",
	} {
		if res := call(path, ""); res.Code != 400 {
			t.Fatalf("无租户仍可访问 %s: %+v", path, res)
		}
		if res := call(path, "2"); res.Code != 1_002_015_001 {
			t.Fatalf("禁用租户仍可访问 %s: %+v", path, res)
		}
	}
	for _, path := range []string{
		"/rpc-api/system/permission/has-any-roles?userId=x&roles=editor",
		"/rpc-api/system/permission/has-any-roles?userId=8",
		"/rpc-api/system/permission/user-role-id-list-by-role-id?roleIds=3,x",
	} {
		if res := call(path, "1"); res.Code != 400 {
			t.Fatalf("非法参数未拒绝 %s: %+v", path, res)
		}
	}
	if res := call("/rpc-api/system/permission/has-any-roles?userId=8&roles=", "1"); res.Code != 0 || string(res.Data) != "true" {
		t.Fatalf("空角色数组应放行：%+v", res)
	}
}
