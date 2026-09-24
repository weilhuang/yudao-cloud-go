package directory

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDictAdminRoutes(t *testing.T) {
	r := gin.New()
	Mount(r, nil, &Service{})
	want := map[string]bool{
		"GET /admin-api/system/dict-type/get":            false,
		"DELETE /admin-api/system/dict-type/delete-list": false,
		"GET /admin-api/system/dict-type/export-excel":   false,
		"GET /admin-api/system/dict-data/get":            false,
		"DELETE /admin-api/system/dict-data/delete-list": false,
		"GET /admin-api/system/dict-data/export-excel":   false,
	}
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("未注册字典页路由 %s", route)
		}
	}
}

func TestDictTypeQueryCreateTimeFormats(t *testing.T) {
	for _, raw := range []string{
		"/page?createTime=2026-09-23+09%3A00%3A00&createTime=2026-09-23+12%3A00%3A00",
		"/page?createTime[0]=2026-09-23+09%3A00%3A00&createTime[1]=2026-09-23+12%3A00%3A00",
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, raw, nil)
		query, ok := dictTypeQuery(c)
		if !ok || query.CreateStart == nil || query.CreateEnd == nil ||
			query.CreateStart.Format("2006-01-02 15:04:05") != "2026-09-23 09:00:00" ||
			query.CreateEnd.Format("2006-01-02 15:04:05") != "2026-09-23 12:00:00" {
			t.Fatalf("创建时间数组未按 Java 绑定：%s %+v", raw, query)
		}
	}
}

func TestDictDataQueryValidation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/page?status=0&pageNo=2&pageSize=20", nil)
	pageNo, pageSize, status, ok := dictPageQuery(c)
	if !ok || pageNo != 2 || pageSize != 20 || status == nil || *status != 0 {
		t.Fatalf("分页/状态解析错误：%d %d %v %v", pageNo, pageSize, status, ok)
	}
	recorder := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/page?status=2", nil)
	if dictDataFieldsValid(c) || recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"code":400`) {
		t.Fatal("非法字典状态未拒绝")
	}
}
