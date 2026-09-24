//go:build integration

package rolepostrpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestRolePostMySQLContract 在真实 MySQL 上验证租户、逻辑删除和停用记录边界。
func TestRolePostMySQLContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("rolepost"),
		mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dsn, err := container.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, rolePostSchema); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	MountRPC(r, &Service{Reader: &MySQL{DB: db}}, func(_ context.Context, tenantID int64, _ time.Time) error {
		if tenantID != 1 && tenantID != 2 {
			t.Fatalf("未预期租户：%d", tenantID)
		}
		return nil
	})
	call := func(path, tenant string) testResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("tenant-id", tenant)
		r.ServeHTTP(w, req)
		var result testResult
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("%s: HTTP %d %s", path, w.Code, w.Body.String())
		}
		return result
	}
	// 相同 ID 查询仅返回当前租户的有效记录；停用记录仍在 list 里。
	posts := call("/rpc-api/system/post/list?ids=1,2,3,4", "1")
	var postList []Post
	if posts.Code != 0 || json.Unmarshal(posts.Data, &postList) != nil || len(postList) != 2 ||
		postList[0].ID != 1 || postList[1].ID != 2 || postList[1].Status != 1 {
		t.Fatalf("岗位租户/逻辑删除边界错误：%+v", posts)
	}
	roles := call("/rpc-api/system/role/list?ids=11,12,13,14", "1")
	var roleList []Role
	if roles.Code != 0 || json.Unmarshal(roles.Data, &roleList) != nil || len(roleList) != 2 ||
		roleList[0].ID != 11 || roleList[1].ID != 12 || roleList[1].Status != 1 {
		t.Fatalf("角色租户/逻辑删除边界错误：%+v", roles)
	}
	for _, path := range []string{"/rpc-api/system/role/get?id=13", "/rpc-api/system/role/get?id=14"} {
		result := call(path, "1")
		if result.Code != 0 || string(result.Data) != "null" {
			t.Fatalf("已删除或跨租户角色应为 null：%+v", result)
		}
	}
	if result := call("/rpc-api/system/post/valid?ids=2", "1"); result.Code != 1_002_005_001 {
		t.Fatalf("停用岗位被误判为合法：%+v", result)
	}
	if result := call("/rpc-api/system/role/valid?ids=12", "1"); result.Code != 1_002_002_004 {
		t.Fatalf("停用角色被误判为合法：%+v", result)
	}
	if result := call("/rpc-api/system/post/valid?ids=4", "1"); result.Code != 1_002_005_000 {
		t.Fatalf("跨租户岗位应视为不存在：%+v", result)
	}
	if result := call("/rpc-api/system/role/valid?ids=14", "1"); result.Code != 1_002_002_000 {
		t.Fatalf("跨租户角色应视为不存在：%+v", result)
	}
	if result := call("/rpc-api/system/role/get?id=14", "2"); result.Code != 0 || string(result.Data) == "null" {
		t.Fatalf("本租户角色不可见：%+v", result)
	}
}

const rolePostSchema = `
CREATE TABLE system_post (id BIGINT PRIMARY KEY, name VARCHAR(50), code VARCHAR(64), sort INT,
  status INT, tenant_id BIGINT, deleted BIT(1));
CREATE TABLE system_role (id BIGINT PRIMARY KEY, name VARCHAR(50), code VARCHAR(100), sort INT,
  status INT, tenant_id BIGINT, deleted BIT(1));
INSERT INTO system_post VALUES
  (1,'开发','dev',1,0,1,0),(2,'测试','test',2,1,1,0),
  (3,'删除','deleted',3,0,1,1),(4,'外部','other',4,0,2,0);
INSERT INTO system_role VALUES
  (11,'管理员','admin',1,0,1,0),(12,'只读','readonly',2,1,1,0),
  (13,'删除','deleted',3,0,1,1),(14,'外部','other',4,0,2,0);
`
