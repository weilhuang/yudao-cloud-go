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

func TestOAuthUserPartialUpdateMySQL(t *testing.T) {
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
INSERT INTO system_post (id, name, code, sort, status, deleted, tenant_id) VALUES
  (2, '开发', 'dev', 1, 1, 0, 1),
  (3, '测试', 'qa', 2, 0, 0, 1),
  (4, '外租户', 'out', 1, 0, 0, 2);
INSERT INTO system_users (id, username, password, nickname, dept_id, post_ids, email, mobile, sex, avatar, status, deleted, tenant_id) VALUES
  (1, 'neo', 'x', '旧名', 10, '[3,2,4]', 'old@example.com', '15601691300', 1, 'http://avatar', 0, 0, 1),
  (2, 'other', 'x', '别人', NULL, NULL, 'other@example.com', '15601691301', 1, '', 0, 0, 1),
  (3, 'far', 'x', '外', NULL, NULL, 'old@example.com', '15601691300', 1, '', 0, 0, 2);
`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	posts, err := store.PostsByIDs(ctx, 1, []int64{3, 2, 4, 99})
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0].Name != "测试" || posts[1].Name != "开发" {
		t.Fatalf("岗位顺序或停用岗位不对: %+v", posts)
	}
	nick := "新名"
	if err := store.UpdateOAuthUser(ctx, 1, 1, &nick, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var nickname, email, mobile, avatar string
	var sex int
	if err := db.QueryRow(`SELECT nickname, email, mobile, sex, avatar FROM system_users WHERE id=1`).Scan(&nickname, &email, &mobile, &sex, &avatar); err != nil {
		t.Fatal(err)
	}
	if nickname != "新名" || email != "old@example.com" || mobile != "15601691300" || sex != 1 || avatar != "http://avatar" {
		t.Fatalf("未提交字段被改写: %s %s %s %d %s", nickname, email, mobile, sex, avatar)
	}
	taken := "other@example.com"
	if err := store.UpdateOAuthUser(ctx, 1, 1, nil, &taken, nil, nil); err == nil || err.(*Error).Msg != "邮箱已经存在" {
		t.Fatal(err)
	}
	takenMobile := "15601691301"
	if err := store.UpdateOAuthUser(ctx, 1, 1, nil, nil, &takenMobile, nil); err == nil || err.(*Error).Msg != "手机号已经存在" {
		t.Fatal(err)
	}
	same := "old@example.com"
	if err := store.UpdateOAuthUser(ctx, 1, 1, nil, &same, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOAuthUser(ctx, 1, 99, &nick, nil, nil, nil); err == nil || err.(*Error).Code != codeUserNotExists {
		t.Fatal(err)
	}
}
