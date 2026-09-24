//go:build integration

package directory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// delayedRoleMenuStore 把授权请求暂停在服务层旧套餐校验之后、数据库事务之前。
type delayedRoleMenuStore struct {
	AccessStore
	entered chan struct{}
	release chan struct{}
}

func (m *delayedRoleMenuStore) ReplaceRoleMenus(ctx context.Context, tenantID, roleID int64, menuIDs []int64) error {
	close(m.entered)
	select {
	case <-m.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return m.AccessStore.ReplaceRoleMenus(ctx, tenantID, roleID, menuIDs)
}

// TestTenantPackageChangeCannotBeUndoneByStaleRoleGrant 固定两次请求的交错顺序，
// 验证旧套餐下通过服务预校验的菜单不会在套餐变更提交后被回插。
func TestTenantPackageChangeCannotBeUndoneByStaleRoleGrant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("role_package_race"),
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
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, userSchema+roleTenantSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	access := &delayedRoleMenuStore{AccessStore: store, entered: make(chan struct{}), release: make(chan struct{})}
	svc := &Service{Access: access, Tenants: store}
	result := make(chan error, 1)
	go func() {
		result <- svc.AssignRoleMenus(ctx, 1, 11, []int64{101})
	}()
	select {
	case <-access.entered:
	case <-ctx.Done():
		t.Fatal("旧套餐授权请求未到达数据库写入边界")
	}
	// 套餐从仅有菜单 101 改成仅有菜单 202；它应先提交并撤销旧关系。
	updated := sampleTenant(1)
	updated.PackageID = 20
	if _, err := svc.SaveTenant(ctx, updated); err != nil {
		t.Fatal(err)
	}
	close(access.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	var oldCount, foreignCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_role_menu WHERE role_id=11 AND tenant_id=1 AND menu_id=101 AND deleted=0`).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_role_menu WHERE role_id=11 AND tenant_id=2 AND menu_id=202 AND deleted=0`).Scan(&foreignCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 || foreignCount != 1 {
		t.Fatalf("旧套餐权限回插或其他租户关系被修改：old=%d foreign=%d", oldCount, foreignCount)
	}
}
