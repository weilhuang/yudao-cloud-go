//go:build integration

package tenantrpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestTenantMySQLContract 使用与 mini 相同的字段，验证跨租户、软删、停用和过期边界。
func TestTenantMySQLContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("tenantrpc"),
		mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dsn, err := container.ConnectionString(ctx, "parseTime=true", "loc=Local", "multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `
CREATE TABLE system_tenant (
  id BIGINT PRIMARY KEY, name VARCHAR(30) NOT NULL, status TINYINT NOT NULL,
  expire_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0);
INSERT INTO system_tenant VALUES
  (1, '启用', 0, '2099-01-01 00:00:00', 0),
  (2, '停用', 1, '2099-01-01 00:00:00', 0),
  (3, '过期', 0, '2020-01-01 00:00:00', 0),
  (4, '已删除', 0, '2099-01-01 00:00:00', 1);`); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	MountRPC(r, &Service{Reader: &MySQL{DB: db}})
	call := func(path string) testResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("tenant-id", "999") // 即使有其他租户编号，@TenantIgnore 仍查询全局数据。
		r.ServeHTTP(w, req)
		var result testResult
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("%s: HTTP %d %s", path, w.Code, w.Body.String())
		}
		return result
	}
	if result := call("/rpc-api/system/tenant/id-list"); result.Code != 0 || string(result.Data) != "[1,2,3]" {
		t.Fatalf("停用和过期租户应列出，软删租户不应列出：%+v", result)
	}
	for _, tc := range []struct {
		id   int
		code int
	}{
		{1, 0}, {2, 1_002_015_001}, {3, 1_002_015_002}, {4, 1_002_015_000},
	} {
		result := call("/rpc-api/system/tenant/valid?id=" + strconv.Itoa(tc.id))
		if result.Code != tc.code {
			t.Fatalf("id=%d code=%d, want %d: %+v", tc.id, result.Code, tc.code, result)
		}
	}
}
