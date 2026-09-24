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

func TestMenuDetailAndTransactionalDelete(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, menuSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Access: store}
	item, err := store.MenuByID(ctx, 3)
	if err != nil || item == nil || item.CreateTime <= 0 {
		t.Fatalf("菜单详情应包含毫秒时间: item=%+v err=%v", item, err)
	}
	list, err := store.MenuList(ctx, "", nil)
	if err != nil || len(list) != 4 || list[0].CreateTime <= 0 {
		t.Fatalf("菜单列表应包含毫秒时间: list=%+v err=%v", list, err)
	}
	if err := svc.DeleteMenuList(ctx, []int64{1, 3}); err == nil {
		t.Fatal("批量删除包含有子菜单的父级时必须整体拒绝")
	}
	assertMenuDeleted(t, db, 3, false)
	if err := svc.DeleteMenuList(ctx, []int64{3, 999}); err != nil {
		t.Fatal(err)
	}
	assertMenuDeleted(t, db, 3, true)
	assertRoleMenuDeleted(t, db, 3, true)
	if err := svc.DeleteMenu(ctx, 4); err != nil {
		t.Fatal(err)
	}
	assertMenuDeleted(t, db, 4, true)
	assertRoleMenuDeleted(t, db, 4, true)

	// 关系表写入失败时，同一事务内的菜单状态也应回滚。
	if _, err := db.ExecContext(ctx, `DROP TABLE system_role_menu`); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMenuList(ctx, []int64{2}); err == nil {
		t.Fatal("关系表故障必须使批量删除失败")
	}
	assertMenuDeleted(t, db, 2, false)
}

func assertMenuDeleted(t *testing.T, db *sql.DB, id int64, want bool) {
	t.Helper()
	var deleted bool
	if err := db.QueryRow(`SELECT deleted+0 FROM system_menu WHERE id=?`, id).Scan(&deleted); err != nil || deleted != want {
		t.Fatalf("菜单 %d deleted=%v want=%v err=%v", id, deleted, want, err)
	}
}

func assertRoleMenuDeleted(t *testing.T, db *sql.DB, menuID int64, want bool) {
	t.Helper()
	var deleted bool
	if err := db.QueryRow(`SELECT deleted+0 FROM system_role_menu WHERE menu_id=?`, menuID).Scan(&deleted); err != nil || deleted != want {
		t.Fatalf("角色菜单 %d deleted=%v want=%v err=%v", menuID, deleted, want, err)
	}
}

const menuSchema = `
CREATE TABLE system_menu (
  id bigint PRIMARY KEY,
  name varchar(50) NOT NULL,
  permission varchar(100) NULL,
  type tinyint NOT NULL DEFAULT 1,
  sort int NOT NULL DEFAULT 0,
  parent_id bigint NOT NULL DEFAULT 0,
  path varchar(200) NULL,
  icon varchar(100) NULL,
  component varchar(255) NULL,
  component_name varchar(255) NULL,
  status tinyint NOT NULL DEFAULT 0,
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
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_menu (id,name,parent_id) VALUES
 (1,'父级',0),(2,'子级',1),(3,'叶子',2),(4,'独立菜单',0);
INSERT INTO system_role_menu (role_id,menu_id) VALUES (7,3),(7,4);
`
