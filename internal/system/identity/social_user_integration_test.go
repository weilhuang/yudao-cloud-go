//go:build integration

package identity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestSocialBindAndGetOnMySQL(t *testing.T) {
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
	if _, err := db.Exec(`
CREATE TABLE system_social_user (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 type tinyint NOT NULL,
 openid varchar(64) NOT NULL,
 nickname varchar(64) NULL,
 avatar varchar(255) NULL,
 token varchar(255) NULL,
 raw_token_info varchar(1024) NULL,
 raw_user_info varchar(1024) NULL,
 code varchar(64) NULL,
 state varchar(64) NULL,
 tenant_id bigint NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0,
 create_time datetime NULL,
 update_time datetime NULL
);
CREATE TABLE system_social_user_bind (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 user_id bigint NOT NULL,
 user_type tinyint NOT NULL,
 social_type tinyint NOT NULL,
 social_user_id bigint NOT NULL,
 tenant_id bigint NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0,
 create_time datetime NULL
);
INSERT INTO system_social_user (type, openid, nickname, code, state, tenant_id, deleted, create_time)
 VALUES (10, 'gitee-1', '旧昵称', 'code-1', 'state-1', 1, 0, NOW());
INSERT INTO system_social_user_bind (user_id, user_type, social_type, social_user_id, tenant_id, deleted, create_time)
 VALUES (9, 2, 10, 1, 1, 0, NOW());
`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Store: store}
	if err := svc.BindCurrentUser(ctx, 1, 7, 10, "code-1", "state-1"); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := db.QueryRow(`SELECT user_id FROM system_social_user_bind WHERE deleted=0 AND social_user_id=1`).Scan(&userID); err != nil || userID != 7 {
		t.Fatalf("绑定应改到当前用户：%d %v", userID, err)
	}
	item, err := store.SocialUserByID(ctx, 1, 1)
	if err != nil || item == nil || item.OpenID != "gitee-1" {
		t.Fatalf("%+v %v", item, err)
	}
	if other, err := store.SocialUserByID(ctx, 2, 1); err != nil || other != nil {
		t.Fatalf("其他租户不能读到：%+v %v", other, err)
	}
	if err := svc.UnbindCurrentUser(ctx, 1, 7, 10, "missing"); err == nil {
		t.Fatal("不存在的 openid 应失败")
	}
}
