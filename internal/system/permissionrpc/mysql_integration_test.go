//go:build integration

package permissionrpc

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestPermissionMySQL 用实际 MySQL 验证租户、逻辑删除、停用角色和部门后代的查询边界。
func TestPermissionMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("permission"),
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
	if _, err := db.ExecContext(ctx, permissionSchema); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Reader: &MySQL{DB: db}}
	assertBool := func(name string, got bool, err error, want bool) {
		t.Helper()
		if err != nil || got != want {
			t.Fatalf("%s: got=%v err=%v want=%v", name, got, err, want)
		}
	}
	ids, err := svc.UserIDsByRoleIDs(ctx, 1, []int64{1, 2})
	if err != nil || !reflect.DeepEqual(ids, []int64{1, 2}) {
		t.Fatalf("用户角色关系应包含禁用角色，但排除删除记录和其他租户：%v, %v", ids, err)
	}
	ids, err = svc.UserIDsByRoleIDs(ctx, 2, []int64{1, 2, 3})
	if err != nil || len(ids) != 0 {
		t.Fatalf("其他租户读到用户角色关系：%v, %v", ids, err)
	}
	got, err := svc.HasAnyRoles(ctx, 1, 1, []string{"editor"})
	assertBool("启用角色", got, err, true)
	got, err = svc.HasAnyRoles(ctx, 1, 1, []string{"disabled"})
	assertBool("停用角色", got, err, false)
	got, err = svc.HasAnyRoles(ctx, 2, 1, []string{"editor"})
	assertBool("跨租户角色", got, err, false)
	got, err = svc.HasAnyPermissions(ctx, 1, 1, []string{"document:edit"})
	assertBool("精确菜单权限", got, err, true)
	got, err = svc.HasAnyPermissions(ctx, 1, 1, []string{"document:*"})
	assertBool("无通配匹配", got, err, false)
	got, err = svc.HasAnyPermissions(ctx, 1, 1, []string{"disabled:edit"})
	assertBool("停用角色无菜单权限", got, err, false)
	got, err = svc.HasAnyPermissions(ctx, 1, 3, []string{"missing:menu"})
	assertBool("超管兜底", got, err, true)
	got, err = svc.HasAnyPermissions(ctx, 2, 3, []string{"missing:menu"})
	assertBool("跨租户超管不能兜底", got, err, false)
	dept, err := svc.GetDeptDataPermission(ctx, 1, 1)
	if err != nil || dept.All || dept.Self || !reflect.DeepEqual(dept.DeptIDs, []int64{10, 11, 20}) {
		t.Fatalf("部门子树和自定义范围错误：%+v, %v", dept, err)
	}
	dept, err = svc.GetDeptDataPermission(ctx, 2, 1)
	if err != nil || !dept.Self || len(dept.DeptIDs) != 0 {
		t.Fatalf("其他租户不能读取角色数据范围：%+v, %v", dept, err)
	}
}

const permissionSchema = `
CREATE TABLE system_user_role (id BIGINT PRIMARY KEY, user_id BIGINT, role_id BIGINT, tenant_id BIGINT, deleted BIT(1));
CREATE TABLE system_role (id BIGINT PRIMARY KEY, code VARCHAR(100), status INT, data_scope INT,
  data_scope_dept_ids VARCHAR(500), tenant_id BIGINT, deleted BIT(1));
CREATE TABLE system_role_menu (id BIGINT PRIMARY KEY, role_id BIGINT, menu_id BIGINT, tenant_id BIGINT, deleted BIT(1));
CREATE TABLE system_menu (id BIGINT PRIMARY KEY, permission VARCHAR(100), status INT, deleted BIT(1));
CREATE TABLE system_users (id BIGINT PRIMARY KEY, dept_id BIGINT, tenant_id BIGINT, deleted BIT(1));
CREATE TABLE system_dept (id BIGINT PRIMARY KEY, parent_id BIGINT, tenant_id BIGINT, deleted BIT(1));
INSERT INTO system_users VALUES (1,10,1,0),(2,NULL,1,0),(3,NULL,1,0),(4,21,2,0);
INSERT INTO system_role VALUES
 (1,'editor',0,2,'[20]',1,0),(2,'disabled',1,1,'',1,0),
 (3,'super_admin',0,1,'',1,0),(4,'child',0,4,'',1,0),
 (5,'other_tenant',0,1,'',2,0),(6,'deleted',0,1,'',1,1);
INSERT INTO system_user_role VALUES
 (1,1,1,1,0),(2,1,2,1,0),(3,1,4,1,0),(4,2,1,1,0),
 (5,3,3,1,0),(6,4,5,2,0),(7,1,6,1,0),(8,1,3,1,1);
INSERT INTO system_menu VALUES (10,'document:edit',1,0),(11,'disabled:edit',0,0),(12,'deleted:menu',0,1);
INSERT INTO system_role_menu VALUES (1,1,10,1,0),(2,2,11,1,0),(3,1,12,1,0),(4,5,10,2,0);
INSERT INTO system_dept VALUES (10,0,1,0),(11,10,1,0),(12,10,1,1),(20,0,1,0),(21,10,2,0);
`
