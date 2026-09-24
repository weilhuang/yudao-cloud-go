package demo01

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDemo01Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, nil, &Service{})
	want := map[string]bool{
		"POST /admin-api/infra/demo01-contact/create":        false,
		"PUT /admin-api/infra/demo01-contact/update":         false,
		"DELETE /admin-api/infra/demo01-contact/delete":      false,
		"DELETE /admin-api/infra/demo01-contact/delete-list": false,
		"GET /admin-api/infra/demo01-contact/get":            false,
		"GET /admin-api/infra/demo01-contact/page":           false,
		"GET /admin-api/infra/demo01-contact/export-excel":   false,
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

func TestParseBirthday(t *testing.T) {
	millis, ok := parseBirthday([]byte("1700000000000"))
	if !ok || millis != 1700000000000 {
		t.Fatal(millis, ok)
	}
	parsed, ok := parseBirthday([]byte(`"2023-11-07 00:00:00"`))
	if !ok || excelTime(parsed) != "2023-11-07 00:00:00" {
		t.Fatal(parsed, ok, excelTime(parsed))
	}
	if _, ok := parseBirthday([]byte("null")); ok {
		t.Fatal("null 不能当成出生年")
	}
}
