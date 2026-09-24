//go:build integration

package directory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/go-sql-driver/mysql"
)

func TestUserCreateAndPage(t *testing.T) {
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
	if _, err := db.Exec(userSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Writer: store, BcryptCost: 4}
	id, err := svc.CreateUser(ctx, 1, UserSave{Username: "neo", Nickname: "尼奥", Password: "admin123"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.UserPage(ctx, 1, UserQuery{PageNo: 1, PageSize: 10, Username: "neo"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.List[0].ID != id || page.List[0].Nickname != "尼奥" {
		t.Fatalf("%+v", page)
	}
	var hash string
	if err := db.QueryRow(`SELECT password FROM system_users WHERE id=?`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin123")) != nil {
		t.Fatal("库中的密码不是 BCrypt")
	}
}

// TestUserPostWriteThrough 验证用户主表与岗位关联表同事务更新，失败不留下半笔数据。
func TestUserPostWriteThrough(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
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
	if _, err := db.ExecContext(ctx, userSchema+`
INSERT INTO system_post (id,name,code,status,deleted,tenant_id) VALUES
 (7,'研发','dev',0,0,1),(8,'测试','qa',0,0,1),(9,'禁用','off',1,0,1),(10,'其他租户','other',0,0,2);`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Writer: store, Access: store, BcryptCost: 4}
	id, err := svc.CreateUser(ctx, 1, UserSave{Username: "neo", Nickname: "尼奥", Password: "admin123", PostIDs: []int64{7, 8, 7}})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.UserGet(ctx, 1, id)
	if err != nil || user == nil || len(user.PostIDs) != 2 || user.PostIDs[0] != 7 || user.PostIDs[1] != 8 {
		t.Fatalf("用户主表岗位未写入：%+v %v", user, err)
	}
	assertPosts := func(want7, want8 int) {
		t.Helper()
		var n7, n8 int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_user_post WHERE user_id=? AND tenant_id=1 AND post_id=7 AND deleted=0`, id).Scan(&n7); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_user_post WHERE user_id=? AND tenant_id=1 AND post_id=8 AND deleted=0`, id).Scan(&n8); err != nil {
			t.Fatal(err)
		}
		if n7 != want7 || n8 != want8 {
			t.Fatalf("岗位关系错误：7=%d 8=%d，预期 %d %d", n7, n8, want7, want8)
		}
	}
	assertPosts(1, 1)
	if err := svc.UpdateUser(ctx, 1, UserSave{ID: id, Username: "neo", Nickname: "尼奥2", PostIDs: []int64{8, 8}}); err != nil {
		t.Fatal(err)
	}
	assertPosts(0, 1)
	users, err := store.RPCUsers(ctx, 1, RPCUserFilter{PostIDs: []int64{8}})
	if err != nil || len(users) != 1 || users[0].ID != id || len(users[0].PostIDs) != 1 || users[0].PostIDs[0] != 8 {
		t.Fatalf("RPC 岗位列表与主表不同步：%+v %v", users, err)
	}
	users, err = store.RPCUsers(ctx, 2, RPCUserFilter{PostIDs: []int64{8}})
	if err != nil || len(users) != 0 {
		t.Fatalf("其他租户看到了岗位用户：%+v %v", users, err)
	}
	if err := svc.UpdateUser(ctx, 1, UserSave{ID: id, Username: "neo", Nickname: "非法岗位", PostIDs: []int64{9}}); rpcErrorCode(err) != 1_002_005_001 {
		t.Fatalf("禁用岗位未拒绝：%v", err)
	}
	if _, err := svc.CreateUser(ctx, 1, UserSave{Username: "bad", Nickname: "跨租户", Password: "admin123", PostIDs: []int64{10}}); rpcErrorCode(err) != 1_002_005_000 {
		t.Fatalf("跨租户岗位未拒绝：%v", err)
	}
	assertPosts(0, 1)

	// 人为移除关联表，迫使事务第二步失败，观察用户主表的新增和修改都回滚。
	if _, err := db.ExecContext(ctx, `DROP TABLE system_user_post`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, 1, UserSave{Username: "rollback", Nickname: "回滚", Password: "admin123", PostIDs: []int64{7}}); err == nil {
		t.Fatal("关联写入失败后创建仍成功")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_users WHERE username='rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("创建事务未回滚：count=%d err=%v", count, err)
	}
	if err := svc.UpdateUser(ctx, 1, UserSave{ID: id, Username: "neo", Nickname: "应回滚", PostIDs: []int64{7}}); err == nil {
		t.Fatal("关联读取失败后修改仍成功")
	}
	user, err = store.UserGet(ctx, 1, id)
	if err != nil || user.Nickname != "尼奥2" || len(user.PostIDs) != 1 || user.PostIDs[0] != 8 {
		t.Fatalf("修改事务未回滚：%+v %v", user, err)
	}
}

func rpcErrorCode(err error) int {
	if biz, ok := err.(*Error); ok {
		return biz.Code
	}
	return -1
}

const userSchema = `
CREATE TABLE system_dept (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  name varchar(30) NOT NULL,
  parent_id bigint NOT NULL DEFAULT 0,
  sort int NOT NULL DEFAULT 0,
  leader_user_id bigint NULL,
  phone varchar(11) NULL,
  email varchar(50) NULL,
  status tinyint NOT NULL,
  create_time datetime NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_users (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  username varchar(30) NOT NULL,
  password varchar(100) NOT NULL,
  nickname varchar(30) NOT NULL,
  remark varchar(500) NULL,
  dept_id bigint NULL,
  post_ids varchar(255) NULL,
  email varchar(50) NULL,
  mobile varchar(11) NULL,
  sex tinyint NULL,
  avatar varchar(512) NULL,
  status tinyint NOT NULL,
  login_ip varchar(50) NULL,
  login_date datetime NULL,
  create_time datetime NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_user_post (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  post_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_post (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  name varchar(30) NOT NULL,
  code varchar(30) NOT NULL,
  sort int NOT NULL DEFAULT 0,
  status tinyint NOT NULL,
  remark varchar(100) NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
INSERT INTO system_dept (id, name, parent_id, sort, status, create_time, deleted, tenant_id) VALUES (10, '研发', 0, 1, 0, NOW(), 0, 1);
`

func TestUserPageAppliesScopeDeptRoleAndCreateTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
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
	if _, err := db.Exec(userSchema + `
CREATE TABLE system_user_role (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  role_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
INSERT INTO system_dept (id, name, parent_id, sort, status, create_time, deleted, tenant_id) VALUES
  (11, '研发一组', 10, 1, 0, NOW(), 0, 1),
  (20, '销售', 0, 1, 0, NOW(), 0, 1);
INSERT INTO system_users (id, username, password, nickname, dept_id, status, create_time, deleted, tenant_id) VALUES
  (1, 'a', 'x', '甲', 10, 0, '2026-01-01 08:00:00', 0, 1),
  (2, 'b', 'x', '乙', 11, 0, '2026-02-01 08:00:00', 0, 1),
  (3, 'c', 'x', '丙', 20, 0, '2026-03-01 08:00:00', 0, 1);
INSERT INTO system_user_role (user_id, role_id, deleted, tenant_id) VALUES (1, 9, 0, 1);
`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	ids := func(query UserQuery) []int64 {
		t.Helper()
		page, err := store.UserPage(ctx, 1, query)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]int64, 0, len(page.List))
		for _, user := range page.List {
			got = append(got, user.ID)
		}
		return got
	}
	dept := int64(10)
	if got := ids(UserQuery{DeptID: &dept}); len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("部门应包含子部门：%v", got)
	}
	role := int64(9)
	if got := ids(UserQuery{RoleID: &role}); len(got) != 1 || got[0] != 1 {
		t.Fatalf("角色筛选：%v", got)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	from := time.Date(2026, 2, 1, 0, 0, 0, 0, loc)
	to := time.Date(2026, 2, 28, 23, 59, 59, 0, loc)
	if got := ids(UserQuery{CreatedFrom: &from, CreatedTo: &to}); len(got) != 1 || got[0] != 2 {
		t.Fatalf("创建时间：%v", got)
	}
	if got := ids(UserQuery{Access: &UserAccess{DeptIDs: []int64{20}}}); len(got) != 1 || got[0] != 3 {
		t.Fatalf("部门数据权限：%v", got)
	}
	if got := ids(UserQuery{Access: &UserAccess{Self: true, UserID: 1}}); len(got) != 1 || got[0] != 1 {
		t.Fatalf("仅本人：%v", got)
	}
	if got := ids(UserQuery{Access: &UserAccess{}}); len(got) != 0 {
		t.Fatalf("空范围：%v", got)
	}
	if got := ids(UserQuery{Access: &UserAccess{All: true}}); len(got) != 3 {
		t.Fatalf("全部：%v", got)
	}
	scoped := withUserAccess(ctx, UserAccess{DeptIDs: []int64{10, 11}})
	user, err := store.UserGet(scoped, 1, 3)
	if err != nil || user != nil {
		t.Fatalf("详情不能绕过数据范围：%v %+v", err, user)
	}
	var exported []int64
	if err := store.UserExportRows(ctx, 1, UserQuery{Access: &UserAccess{Self: true, UserID: 2}}, func(user UserDetail) error {
		exported = append(exported, user.ID)
		return nil
	}); err != nil || len(exported) != 1 || exported[0] != 2 {
		t.Fatalf("导出：%v %v", exported, err)
	}
}

func TestUserAndRoleDeleteKeepOtherTenant(t *testing.T) {
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
	if _, err := db.Exec(userSchema + deleteRelationSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	cache := &javaRoleCacheSpy{}
	svc := &Service{Reader: store, Writer: store, Access: store, JavaRoleCache: cache}
	if err := svc.DeleteUserList(ctx, 1, []int64{8, 404}); err != nil {
		t.Fatal(err)
	}
	assertDeleted := func(query string, args ...any) {
		t.Helper()
		var deleted int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&deleted); err != nil || deleted != 1 {
			t.Fatalf("%s deleted=%d err=%v", query, deleted, err)
		}
	}
	assertAlive := func(query string, args ...any) {
		t.Helper()
		var deleted int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&deleted); err != nil || deleted != 0 {
			t.Fatalf("%s 不应删除 deleted=%d err=%v", query, deleted, err)
		}
	}
	assertDeleted(`SELECT deleted+0 FROM system_users WHERE id=8`)
	assertDeleted(`SELECT deleted+0 FROM system_user_role WHERE user_id=8 AND tenant_id=1`)
	assertDeleted(`SELECT deleted+0 FROM system_user_post WHERE user_id=8 AND tenant_id=1`)
	assertAlive(`SELECT deleted+0 FROM system_users WHERE id=9`)
	assertAlive(`SELECT deleted+0 FROM system_user_role WHERE user_id=8 AND tenant_id=2`)
	assertAlive(`SELECT deleted+0 FROM system_user_post WHERE user_id=9 AND tenant_id=2`)
	if len(cache.calls) != 1 || cache.calls[0] != "user:8" {
		t.Fatalf("只应失效本租户已删除用户：%v", cache.calls)
	}

	err = svc.DeleteRoleList(ctx, 1, []int64{11, 12})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_002_003 {
		t.Fatalf("内置角色应让整批失败：%v", err)
	}
	if len(cache.calls) != 1 {
		t.Fatalf("失败的角色批删不应清理缓存：%v", cache.calls)
	}
	assertAlive(`SELECT deleted+0 FROM system_role WHERE id=11`)
	assertAlive(`SELECT deleted+0 FROM system_role_menu WHERE role_id=11`)
	if err := svc.DeleteRoleList(ctx, 1, []int64{11}); err != nil {
		t.Fatal(err)
	}
	assertDeleted(`SELECT deleted+0 FROM system_role WHERE id=11`)
	assertDeleted(`SELECT deleted+0 FROM system_user_role WHERE role_id=11 AND tenant_id=1`)
	assertDeleted(`SELECT deleted+0 FROM system_role_menu WHERE role_id=11 AND tenant_id=1`)
	assertAlive(`SELECT deleted+0 FROM system_role WHERE id=22`)
	assertAlive(`SELECT deleted+0 FROM system_user_role WHERE role_id=11 AND tenant_id=2`)
}

const deleteRelationSchema = `
CREATE TABLE system_user_role (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  role_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
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
CREATE TABLE system_role_menu (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  role_id bigint NOT NULL,
  menu_id bigint NOT NULL,
  tenant_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_users (id, username, password, nickname, status, deleted, tenant_id) VALUES
  (8, 'gone', 'x', '要删', 0, 0, 1),
  (9, 'stay', 'x', '留下', 0, 0, 2);
INSERT INTO system_user_post (user_id, post_id, deleted, tenant_id) VALUES
  (8, 1, 0, 1),
  (9, 1, 0, 2);
INSERT INTO system_role (id, name, code, sort, status, type, deleted, tenant_id) VALUES
  (11, '自定义', 'custom', 1, 0, 2, 0, 1),
  (12, '内置', 'builtin', 1, 0, 1, 0, 1),
  (22, '他租户', 'other', 1, 0, 2, 0, 2);
INSERT INTO system_user_role (user_id, role_id, deleted, tenant_id) VALUES
  (8, 11, 0, 1),
  (8, 11, 0, 2),
  (9, 22, 0, 2);
INSERT INTO system_role_menu (role_id, menu_id, tenant_id, deleted) VALUES
  (11, 3, 1, 0),
  (11, 3, 2, 0);
`
