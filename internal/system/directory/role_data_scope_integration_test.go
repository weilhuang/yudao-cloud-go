//go:build integration

package directory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

// TestRoleDataScopeSQL 对照真实 MySQL 验证角色详情、租户隔离及范围授权的原子写入。
func TestRoleDataScopeSQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("ruoyi-vue-pro"),
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
	if _, err := db.ExecContext(ctx, roleScopeSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Access: store, Depts: store}

	role, err := store.RoleGet(ctx, 1, 7)
	if err != nil || role == nil || role.CreateTime == 0 || role.DataScope != 2 || len(role.DataScopeDeptIDs) != 2 {
		t.Fatalf("角色详情遗漏范围/时间：%+v, %v", role, err)
	}
	for _, id := range []int64{22, 44} {
		role, err := store.RoleGet(ctx, 1, id)
		if err != nil || role != nil {
			t.Fatalf("跨租户或软删除角色仍可读取 id=%d: %+v, %v", id, role, err)
		}
	}
	page, err := store.RolePage(ctx, 1, 1, 10, "", "", nil)
	if err != nil || page.Total != 2 || len(page.List) != 2 || page.List[0].ID != 7 || len(page.List[0].DataScopeDeptIDs) != 2 {
		t.Fatalf("分页范围字段错误：%+v, %v", page, err)
	}
	if err := svc.AssignRoleDataScope(ctx, 1, 7, 2, []int64{10, 10}); err != nil {
		t.Fatal(err)
	}
	assertScopeRow(t, ctx, db, 7, 2, "[10]")
	for _, test := range []struct {
		name string
		role int64
		dept int64
		code int
	}{
		{name: "跨租户部门", role: 7, dept: 20, code: 1_002_004_002},
		{name: "已删除部门", role: 7, dept: 30, code: 1_002_004_002},
		{name: "已删除角色", role: 44, dept: 10, code: 1_002_002_000},
		{name: "跨租户角色", role: 22, dept: 10, code: 1_002_002_000},
		{name: "系统内置角色", role: 33, dept: 10, code: 1_002_002_003},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := svc.AssignRoleDataScope(ctx, 1, test.role, 2, []int64{test.dept})
			biz, _ := err.(*Error)
			if biz == nil || biz.Code != test.code {
				t.Fatalf("错误码：%v，期望 %d", err, test.code)
			}
			assertScopeRow(t, ctx, db, 7, 2, "[10]")
		})
	}
	// 绕过服务预校验直接写存储层，也不能写入其他租户部门。
	if err := store.UpdateRoleDataScope(ctx, 1, 7, 2, []int64{20}); err == nil {
		t.Fatal("存储层未拒绝跨租户部门")
	}
	assertScopeRow(t, ctx, db, 7, 2, "[10]")
	if err := svc.AssignRoleDataScope(ctx, 1, 7, 5, []int64{20}); err != nil {
		t.Fatal(err)
	}
	assertScopeRow(t, ctx, db, 7, 5, "[]")
}

func assertScopeRow(t *testing.T, ctx context.Context, db *sql.DB, roleID int64, wantScope int, wantIDs string) {
	t.Helper()
	var scope int
	var ids string
	if err := db.QueryRowContext(ctx, `SELECT data_scope, data_scope_dept_ids FROM system_role WHERE id=?`, roleID).Scan(&scope, &ids); err != nil {
		t.Fatal(err)
	}
	if scope != wantScope || ids != wantIDs {
		t.Fatalf("数据范围被意外修改：scope=%d ids=%q，期望 %d %q", scope, ids, wantScope, wantIDs)
	}
}

const roleScopeSchema = `
CREATE TABLE system_role (
  id bigint PRIMARY KEY, name varchar(30) NOT NULL, code varchar(100) NOT NULL,
  sort int NOT NULL, data_scope tinyint NOT NULL DEFAULT 1,
  data_scope_dept_ids varchar(500) NOT NULL DEFAULT '', status tinyint NOT NULL,
  type tinyint NOT NULL, remark varchar(500), create_time datetime NOT NULL,
  tenant_id bigint NOT NULL, deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_dept (
  id bigint PRIMARY KEY, tenant_id bigint NOT NULL, deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_role (id,name,code,sort,data_scope,data_scope_dept_ids,status,type,create_time,tenant_id,deleted)
VALUES (7,'租户一角色','reader',1,2,'[10,11]',0,2,'2026-08-01 12:00:00',1,0),
       (22,'租户二角色','writer',2,1,'',0,2,'2026-08-01 12:00:00',2,0),
       (33,'内置角色','admin',3,1,'',0,1,'2026-08-01 12:00:00',1,0),
       (44,'已删除角色','deleted',4,1,'',0,2,'2026-08-01 12:00:00',1,1);
INSERT INTO system_dept (id,tenant_id,deleted) VALUES (10,1,0),(11,1,0),(20,2,0),(30,1,1);
`
