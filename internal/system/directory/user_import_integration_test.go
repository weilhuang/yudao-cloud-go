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

func TestUserImportCommitsValidRowsAndRollsBackOnWriteError(t *testing.T) {
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
	if _, err := db.Exec(userSchema + importSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Writer: store, BcryptCost: 4}
	male, enable := 1, 0
	result, err := svc.ImportUsers(ctx, 1, []ImportUser{
		{Username: "cross", Nickname: "跨租户同名", DeptID: int64ptr(10), Email: "cross@example.com", Mobile: "15601691300", Sex: &male, Status: &enable},
		{Username: "badmobile", Nickname: "坏手机", DeptID: int64ptr(10), Mobile: "12", Sex: &male, Status: &enable},
		{Username: "closed", Nickname: "停用部门", DeptID: int64ptr(11), Mobile: "15601701300", Sex: &male, Status: &enable},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreateUsernames) != 1 || result.CreateUsernames[0] != "cross" {
		t.Fatalf("只应创建合法行：%+v", result)
	}
	if len(result.Failures) != 2 || result.Failures[0].Reason != "mobile: 手机号格式不正确" || result.Failures[1].Reason != "部门(停用)不处于开启状态，不允许选择" {
		t.Fatalf("失败明细：%+v", result.Failures)
	}
	var hash string
	if err := db.QueryRowContext(ctx, `SELECT password FROM system_users WHERE username='cross' AND tenant_id=1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin123")) != nil {
		t.Fatal("初始密码未按配置写入")
	}
	var other int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_users WHERE username='cross' AND tenant_id=2 AND deleted=0`).Scan(&other); err != nil || other != 1 {
		t.Fatalf("其它租户的同名用户被影响：%d %v", other, err)
	}

	again, err := svc.ImportUsers(ctx, 1, []ImportUser{
		{Username: "cross", Nickname: "改名", DeptID: int64ptr(10), Sex: &male, Status: &enable},
	}, false)
	if err != nil || len(again.CreateUsernames) != 0 || len(again.Failures) != 1 || again.Failures[0].Reason != "用户账号已经存在" {
		t.Fatalf("重复导入：%+v %v", again, err)
	}
	sameMobile, err := svc.ImportUsers(ctx, 1, []ImportUser{
		{Username: "cross", Nickname: "不应改", DeptID: int64ptr(10), Mobile: "15601691300", Sex: &male, Status: &enable},
	}, true)
	if err != nil || len(sameMobile.UpdateUsernames) != 0 || len(sameMobile.Failures) != 1 || sameMobile.Failures[0].Reason != "手机号已经存在" {
		t.Fatalf("更新时重复手机号应失败：%+v %v", sameMobile, err)
	}
	updated, err := svc.ImportUsers(ctx, 1, []ImportUser{
		{Username: "cross", Nickname: "改名", DeptID: int64ptr(10), Sex: &male, Status: &enable},
	}, true)
	if err != nil || len(updated.UpdateUsernames) != 1 {
		t.Fatalf("允许更新：%+v %v", updated, err)
	}
	var nickname string
	if err := db.QueryRowContext(ctx, `SELECT nickname FROM system_users WHERE username='cross' AND tenant_id=1`).Scan(&nickname); err != nil || nickname != "改名" {
		t.Fatalf("昵称=%s %v", nickname, err)
	}

	_, err = svc.ImportUsers(ctx, 1, []ImportUser{
		{Username: "neo", Nickname: "应回滚", DeptID: int64ptr(10), Sex: &male, Status: &enable},
		{Username: "broken", Nickname: "坏状态", DeptID: int64ptr(10), Sex: &male},
	}, false)
	if err == nil {
		t.Fatal("空状态写入非空列时应失败并回滚整批")
	}
	var neo int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_users WHERE username='neo'`).Scan(&neo); err != nil || neo != 0 {
		t.Fatalf("回滚后不应留下 neo：%d %v", neo, err)
	}
}

func int64ptr(v int64) *int64 { return &v }

const importSchema = `
CREATE TABLE system_dict_data (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  sort int NOT NULL DEFAULT 0,
  label varchar(100) NOT NULL,
  value varchar(100) NOT NULL,
  dict_type varchar(100) NOT NULL,
  status tinyint NOT NULL DEFAULT 0,
  deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE infra_config (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  config_key varchar(100) NOT NULL,
  value varchar(500) NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_dept (id, name, parent_id, sort, status, deleted, tenant_id) VALUES (11, '停用', 0, 1, 1, 0, 1);
INSERT INTO system_users (id, username, password, nickname, status, deleted, tenant_id) VALUES (20, 'cross', 'other', '租户二', 0, 0, 2);
INSERT INTO infra_config (config_key, value, deleted) VALUES ('system.user.init-password', 'admin123', 0);
`
