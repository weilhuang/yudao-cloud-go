package tenantrpc

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type testResult struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func TestTenantCommonRPCWithoutTenantHeader(t *testing.T) {
	reader := &fakeReader{
		ids:    []int64{1, 2, 9007199254740991},
		tenant: &Tenant{ID: 2, Name: "租户乙", Status: 0, ExpireTime: time.Now().Add(time.Hour)},
	}
	r := gin.New()
	MountRPC(r, &Service{Reader: reader})
	call := func(path, header string) testResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if header != "" {
			req.Header.Set("tenant-id", header)
		}
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var result testResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	// Java 两个方法都 @TenantIgnore；传错 tenant-id 也不能缩窄全局查询。
	list := call("/rpc-api/system/tenant/id-list", "999")
	if list.Code != 0 || list.Msg != "" || string(list.Data) != `[1,2,"9007199254740991"]` {
		t.Fatalf("全局租户编号和 Long 序列化不符：%+v", list)
	}
	for _, header := range []string{"", "999", "not-a-number"} {
		result := call("/rpc-api/system/tenant/valid?id=2", header)
		if result.Code != 0 || result.Msg != "" || string(result.Data) != "true" {
			t.Fatalf("tenant-id=%q 导致校验失败：%+v", header, result)
		}
	}
	reader.tenant.Status = 1
	result := call("/rpc-api/system/tenant/valid?id=2", "")
	if result.Code != 1_002_015_001 || result.Msg != "名字为【租户乙】的租户已被禁用" || string(result.Data) != "null" {
		t.Fatalf("停用业务错误不符：%+v", result)
	}
	reader.tenant.Status = 0
	reader.tenant.ExpireTime = time.Now().Add(-time.Hour)
	result = call("/rpc-api/system/tenant/valid?id=2", "")
	if result.Code != 1_002_015_002 || result.Msg != "名字为【租户乙】的租户已过期" || string(result.Data) != "null" {
		t.Fatalf("过期业务错误不符：%+v", result)
	}
	reader.tenant = nil
	result = call("/rpc-api/system/tenant/valid?id=2", "")
	if result.Code != 1_002_015_000 || result.Msg != "租户不存在" || string(result.Data) != "null" {
		t.Fatalf("不存在业务错误不符：%+v", result)
	}
	for _, tc := range []struct{ path, msg string }{
		{"/rpc-api/system/tenant/valid", "请求参数缺失:id"},
		{"/rpc-api/system/tenant/valid?id=", "请求参数缺失:id"},
		{"/rpc-api/system/tenant/valid?id=abc", "请求参数类型错误:id"},
	} {
		result := call(tc.path, "")
		if result.Code != 400 || result.Msg != tc.msg || string(result.Data) != "null" {
			t.Fatalf("%s 参数错误不符：%+v", tc.path, result)
		}
	}
	reader.listErr = errors.New("secret dsn")
	result = call("/rpc-api/system/tenant/id-list", "")
	if result.Code != 500 || result.Msg != "系统异常" || string(result.Data) != "null" {
		t.Fatalf("数据库错误泄露或包体不符：%+v", result)
	}
}
