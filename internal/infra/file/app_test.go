package file

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAppFileRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, nil, &Service{}, UploadLimits{})
	want := map[string]bool{
		"POST /app-api/infra/file/upload":       false,
		"GET /app-api/infra/file/presigned-url": false,
		"POST /app-api/infra/file/create":       false,
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
