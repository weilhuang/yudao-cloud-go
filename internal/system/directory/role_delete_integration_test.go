//go:build integration

package directory

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestRoleDeleteCleansRelations 验证角色及两类授权关系同事务删除，并守住租户边界。
func TestRoleDeleteCleansRelations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
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
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, userSchema+roleTenantSchema+`
INSERT INTO system_role (id, name, code, sort, status, type, deleted, tenant_id)
 VALUES (44, '回滚角色', 'rollback', 0, 0, 2, 0, 1),
        (55, '内置角色', 'builtin', 0, 0, 1, 0, 1),
        (66, '并发角色', 'concurrent', 0, 0, 2, 0, 1);
INSERT INTO system_user_role (user_id, role_id, tenant_id, deleted)
 VALUES (7, 11, 2, 0), (7, 44, 1, 0);
INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted)
 VALUES (44, 101, 1, 0);`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Access: store}

	if code := rpcErrorCode(svc.DeleteRole(ctx, 2, 11)); code != 1_002_002_000 {
		t.Fatalf("其他租户不应删除角色：%d", code)
	}
	if code := rpcErrorCode(svc.DeleteRole(ctx, 1, 55)); code != 1_002_002_003 {
		t.Fatalf("内置角色不应删除：%d", code)
	}
	if err := svc.DeleteRole(ctx, 1, 11); err != nil {
		t.Fatal(err)
	}
	assertRoleDeleted(t, db, 11, 1, 1)
	assertRoleRelationState(t, db, "system_user_role", 11, 1, 1, 1)
	assertRoleRelationState(t, db, "system_role_menu", 11, 1, 1, 1)
	// 跨租户脏关系仍属于租户 2；角色和关系的租户必须同时匹配才能修改。
	assertRoleRelationState(t, db, "system_user_role", 11, 2, 1, 0)
	assertRoleRelationState(t, db, "system_role_menu", 11, 2, 1, 0)
	assertRoleDeleted(t, db, 22, 2, 0)
	assertRoleDeleted(t, db, 55, 1, 0)
	if code := rpcErrorCode(store.ReplaceRoleMenus(ctx, 1, 11, []int64{101})); code != 1_002_002_000 {
		t.Fatalf("已删除角色不应重新获菜单：%d", code)
	}
	if code := rpcErrorCode(store.ReplaceUserRoles(ctx, 1, 7, []int64{11})); code != 1_002_002_000 {
		t.Fatalf("已删除角色不应重新授予用户：%d", code)
	}
	assertRoleRelationState(t, db, "system_user_role", 11, 1, 1, 1)
	assertRoleRelationState(t, db, "system_role_menu", 11, 1, 1, 1)

	// 模拟另一个事务先删除角色，授权写入必须等待角色行锁并在提交后重新校验。
	holdTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holdTx.ExecContext(ctx, `UPDATE system_role SET deleted=1 WHERE id=66 AND tenant_id=1`); err != nil {
		_ = holdTx.Rollback()
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- store.ReplaceRoleMenus(ctx, 1, 66, []int64{101})
	}()
	go func() {
		defer wg.Done()
		results <- store.ReplaceUserRoles(ctx, 1, 7, []int64{66})
	}()
	if err := holdTx.Commit(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if code := rpcErrorCode(err); code != 1_002_002_000 {
			t.Fatalf("并发删除后的授权写入未拒绝：code=%d err=%v", code, err)
		}
	}
	assertRoleRelationState(t, db, "system_user_role", 66, 1, 0, 0)
	assertRoleRelationState(t, db, "system_role_menu", 66, 1, 0, 0)

	// 关系表故障必须回滚已经执行的角色及用户关系软删除。
	if _, err := db.ExecContext(ctx, `DROP TABLE system_role_menu`); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRole(ctx, 1, 44); err == nil {
		t.Fatal("角色菜单关系写入失败时，删除不应成功")
	}
	assertRoleDeleted(t, db, 44, 1, 0)
	assertRoleRelationState(t, db, "system_user_role", 44, 1, 1, 0)
}

func assertRoleDeleted(t *testing.T, db *sql.DB, roleID, tenantID int64, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT deleted+0 FROM system_role WHERE id=? AND tenant_id=?`, roleID, tenantID).Scan(&got); err != nil || got != want {
		t.Fatalf("角色 %d 租户 %d 的 deleted=%d，期望 %d，err=%v", roleID, tenantID, got, want, err)
	}
}

func assertRoleRelationState(t *testing.T, db *sql.DB, table string, roleID, tenantID int64, wantCount, wantDeleted int) {
	t.Helper()
	// table 只从本文件的固定常量传入，不拼接外部请求。
	var count, deleted int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(deleted+0),0) FROM `+table+` WHERE role_id=? AND tenant_id=?`, roleID, tenantID).Scan(&count, &deleted); err != nil {
		t.Fatal(err)
	}
	if count != wantCount || deleted != wantDeleted {
		t.Fatalf("%s 角色 %d 租户 %d：共 %d 条、删除 %d 条，期望 %d/%d", table, roleID, tenantID, count, deleted, wantCount, wantDeleted)
	}
}
