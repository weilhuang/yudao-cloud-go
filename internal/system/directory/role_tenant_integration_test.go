//go:build integration

package directory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"

	_ "github.com/go-sql-driver/mysql"
)

// TestRoleTenantIsolation 用两套租户数据验证写入前校验与读取时兜底。
// 故意保留一条跨租户脏关系，确保历史错误授权也无法变成 super_admin 权限。
func TestRoleTenantIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"),
		mysql.WithUsername("root"),
		mysql.WithPassword("123456"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })
	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, userSchema+roleTenantSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Access: store, Tenants: store}

	// 角色 22 属于租户 2。租户 1 的用户即使猜到编号，也不能获授该角色。
	err = svc.AssignUserRoles(ctx, 1, 7, []int64{11, 22})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_002_000 {
		t.Fatalf("跨租户分配未拒绝：%v", err)
	}
	var existing int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_user_role WHERE user_id=7 AND deleted=0`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 2 {
		t.Fatalf("失败分配不应修改原授权：%d", existing)
	}

	roles, err := (&auth.MySQL{DB: db}).RolesByUser(ctx, 7)
	if err != nil || len(roles) != 1 || roles[0].ID != 11 {
		t.Fatalf("权限读取混入跨租户超管：%+v, %v", roles, err)
	}
	ids, err := store.UserRoleIDs(ctx, 1, 7)
	if err != nil || len(ids) != 1 || ids[0] != 11 {
		t.Fatalf("授权列表混入跨租户角色：%v, %v", ids, err)
	}
	ids, err = store.UserRoleIDs(ctx, 2, 7)
	if err != nil || len(ids) != 0 {
		t.Fatalf("其他租户不应读取当前用户授权：%v, %v", ids, err)
	}
	menuIDs, err := store.RoleMenuIDs(ctx, 1, 11)
	if err != nil || len(menuIDs) != 1 || menuIDs[0] != 101 {
		t.Fatalf("角色菜单列表混入其他租户关系：%v, %v", menuIDs, err)
	}
	menuIDs, err = store.RoleMenuIDs(ctx, 1, 22)
	if err != nil || len(menuIDs) != 0 {
		t.Fatalf("其他租户角色菜单不应可见：%v, %v", menuIDs, err)
	}
	menus, err := (&auth.MySQL{DB: db}).MenusByRole(ctx, []int64{11}, false)
	if err != nil || len(menus) != 1 || menus[0].ID != 101 {
		t.Fatalf("菜单权限混入其他租户关系：%+v, %v", menus, err)
	}

	// 两个普通租户使用不同套餐；Java 基线会静默移除请求中的套餐外菜单。
	if err := svc.AssignRoleMenus(ctx, 1, 11, []int64{101, 202}); err != nil {
		t.Fatal(err)
	}
	menuIDs, err = store.RoleMenuIDs(ctx, 1, 11)
	if err != nil || len(menuIDs) != 1 || menuIDs[0] != 101 {
		t.Fatalf("租户一越过套餐菜单范围：%v, %v", menuIDs, err)
	}
	if err := svc.AssignRoleMenus(ctx, 2, 22, []int64{101, 202}); err != nil {
		t.Fatal(err)
	}
	menuIDs, err = store.RoleMenuIDs(ctx, 2, 22)
	if err != nil || len(menuIDs) != 1 || menuIDs[0] != 202 {
		t.Fatalf("租户二越过套餐菜单范围：%v, %v", menuIDs, err)
	}
	// 系统租户 package_id=0，可分配全部已存在菜单，但不能写入不存在的菜单编号。
	if err := svc.AssignRoleMenus(ctx, 3, 33, []int64{101, 202, 303, 999}); err != nil {
		t.Fatal(err)
	}
	menuIDs, err = store.RoleMenuIDs(ctx, 3, 33)
	if err != nil || len(menuIDs) != 3 {
		t.Fatalf("系统租户菜单范围错误：%v, %v", menuIDs, err)
	}
	menuSet := make(map[int64]bool, len(menuIDs))
	for _, id := range menuIDs {
		menuSet[id] = true
	}
	if !menuSet[101] || !menuSet[202] || !menuSet[303] || menuSet[999] {
		t.Fatalf("系统租户菜单集合错误：%v", menuIDs)
	}

	if err := svc.AssignUserRoles(ctx, 1, 7, []int64{11}); err != nil {
		t.Fatal(err)
	}
	roles, err = (&auth.MySQL{DB: db}).RolesByUser(ctx, 7)
	if err != nil || len(roles) != 1 || roles[0].ID != 11 {
		t.Fatalf("本租户授权应正常生效：%+v, %v", roles, err)
	}
}

const roleTenantSchema = `
CREATE TABLE system_role (
  id bigint PRIMARY KEY,
  name varchar(50) NOT NULL,
  code varchar(100) NOT NULL,
  sort int NOT NULL,
  status tinyint NOT NULL,
  type tinyint NOT NULL,
  remark varchar(500) NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_user_role (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  role_id bigint NOT NULL,
  tenant_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  create_time datetime NULL
);
CREATE TABLE system_menu (
  id bigint PRIMARY KEY,
  parent_id bigint NOT NULL DEFAULT 0,
  name varchar(50) NOT NULL,
  permission varchar(100) NULL,
  type tinyint NOT NULL,
  sort int NOT NULL DEFAULT 0,
  path varchar(100) NULL,
  icon varchar(100) NULL,
  component varchar(100) NULL,
  component_name varchar(100) NULL,
  status tinyint NOT NULL,
  visible bit(1) NOT NULL DEFAULT 1,
  keep_alive bit(1) NOT NULL DEFAULT 0,
  always_show bit(1) NOT NULL DEFAULT 0,
  deleted bit(1) NOT NULL DEFAULT 0,
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE system_role_menu (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  role_id bigint NOT NULL,
  menu_id bigint NOT NULL,
  tenant_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  create_time datetime NULL
);
CREATE TABLE system_tenant (
  id bigint PRIMARY KEY,
  name varchar(50) NOT NULL,
  contact_name varchar(50) NULL,
  contact_mobile varchar(30) NULL,
  status tinyint NOT NULL,
  websites varchar(512) NULL,
  package_id bigint NOT NULL,
  expire_time datetime NULL,
  account_count int NOT NULL DEFAULT 0,
  create_time datetime NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_tenant_package (
  id bigint PRIMARY KEY,
  name varchar(50) NOT NULL,
  status tinyint NOT NULL,
  remark varchar(500) NULL,
  menu_ids varchar(512) NOT NULL,
  create_time datetime NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_users (id, username, password, nickname, status, deleted, tenant_id)
  VALUES (7, 'tenant-one-user', 'unused', '租户一', 0, 0, 1);
INSERT INTO system_role (id, name, code, sort, status, type, deleted, tenant_id)
  VALUES (11, '普通角色', 'reader', 0, 0, 2, 0, 1),
         (22, '其他租户超管', 'super_admin', 0, 0, 1, 0, 2),
         (33, '系统租户角色', 'system_reader', 0, 0, 2, 0, 3);
INSERT INTO system_user_role (user_id, role_id, tenant_id, deleted)
  VALUES (7, 11, 1, 0),
         (7, 22, 1, 0);
INSERT INTO system_menu (id, name, permission, type, status, deleted)
  VALUES (101, '本租户菜单', 'system:user:query', 3, 0, 0),
         (202, '其他租户菜单', 'system:tenant:delete', 3, 0, 0),
         (303, '系统租户菜单', 'system:menu:create', 3, 0, 0);
INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted)
  VALUES (11, 101, 1, 0),
         (11, 202, 2, 0),
         (22, 202, 2, 0);
INSERT INTO system_tenant (id, name, status, package_id, deleted)
  VALUES (1, '租户一', 0, 10, 0),
         (2, '租户二', 0, 20, 0),
         (3, '系统租户', 0, 0, 0);
INSERT INTO system_tenant_package (id, name, status, menu_ids, deleted)
  VALUES (10, '基础套餐', 0, '[101]', 0),
         (20, '进阶套餐', 0, '[202]', 0);
`
