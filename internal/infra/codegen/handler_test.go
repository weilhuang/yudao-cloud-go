package codegen

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCodegenRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, nil, &Service{})
	want := map[string]bool{
		"GET /admin-api/infra/codegen/db/table/list":  false,
		"GET /admin-api/infra/codegen/table/list":     false,
		"GET /admin-api/infra/codegen/table/page":     false,
		"GET /admin-api/infra/codegen/detail":         false,
		"POST /admin-api/infra/codegen/create-list":   false,
		"PUT /admin-api/infra/codegen/update":         false,
		"PUT /admin-api/infra/codegen/sync-from-db":   false,
		"DELETE /admin-api/infra/codegen/delete":      false,
		"DELETE /admin-api/infra/codegen/delete-list": false,
		"GET /admin-api/infra/codegen/preview":        false,
		"GET /admin-api/infra/codegen/download":       false,
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

func TestUpdateValidation(t *testing.T) {
	scene, name := sceneAdmin, "demo_student"
	body := tableBody{ID: int64ptr(1), Scene: &scene, TableName: &name, TableComment: &name, ModuleName: &name,
		BusinessName: &name, ClassName: &name, ClassComment: &name, Author: &name, TemplateType: intPtr(templateOne), FrontType: intPtr(frontVue3Element)}
	if err := body.validate(); err == nil || err.(*Error).Msg != "上级菜单不能为空，请前往 [修改生成配置 -> 生成信息] 界面，设置“上级菜单”字段" {
		t.Fatal(err)
	}
	menu := int64(10)
	body.ParentMenuID = &menu
	if err := body.validate(); err != nil {
		t.Fatal(err)
	}
}

func int64ptr(v int64) *int64 { return &v }
func intPtr(v int) *int       { return &v }
