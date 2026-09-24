package demo02

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDemo02Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, nil, &Service{})
	want := map[string]bool{
		"POST /admin-api/infra/demo02-category/create":      false,
		"PUT /admin-api/infra/demo02-category/update":       false,
		"DELETE /admin-api/infra/demo02-category/delete":    false,
		"GET /admin-api/infra/demo02-category/get":          false,
		"GET /admin-api/infra/demo02-category/list":         false,
		"GET /admin-api/infra/demo02-category/export-excel": false,
	}
	for _, route := range r.Routes() {
		want[route.Method+" "+route.Path] = true
	}
	for path, seen := range want {
		if !seen {
			t.Fatalf("缺少路由 %s", path)
		}
	}
}
