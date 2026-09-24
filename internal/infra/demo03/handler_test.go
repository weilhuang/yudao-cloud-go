package demo03

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDemo03Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, nil, &Service{})
	want := []string{
		"POST /admin-api/infra/demo03-student-normal/create",
		"GET /admin-api/infra/demo03-student-normal/demo03-course/list-by-student-id",
		"GET /admin-api/infra/demo03-student-normal/demo03-grade/get-by-student-id",
		"POST /admin-api/infra/demo03-student-inner/create",
		"GET /admin-api/infra/demo03-student-inner/demo03-course/list-by-student-id",
		"GET /admin-api/infra/demo03-student-inner/demo03-grade/get-by-student-id",
		"POST /admin-api/infra/demo03-student-erp/create",
		"GET /admin-api/infra/demo03-student-erp/demo03-course/page",
		"POST /admin-api/infra/demo03-student-erp/demo03-course/create",
		"DELETE /admin-api/infra/demo03-student-erp/demo03-course/delete-list",
		"GET /admin-api/infra/demo03-student-erp/demo03-grade/page",
		"POST /admin-api/infra/demo03-student-erp/demo03-grade/create",
		"GET /admin-api/infra/demo03-student-erp/export-excel",
	}
	got := map[string]bool{}
	for _, route := range r.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	for _, path := range want {
		if !got[path] {
			t.Fatalf("缺少路由 %s", path)
		}
	}
	if len(got) != 37 {
		t.Fatalf("路由数量 %d", len(got))
	}
}
