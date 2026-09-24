//go:build integration

package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestAccessTokenPageAndForceLogoutOnMySQL(t *testing.T) {
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
CREATE TABLE system_users (
 id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, username VARCHAR(30) NOT NULL, password VARCHAR(100) NOT NULL,
 nickname VARCHAR(30) NOT NULL, mobile VARCHAR(11) NULL, status TINYINT NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_oauth2_access_token (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, user_id BIGINT NOT NULL, user_type TINYINT NOT NULL,
 user_info VARCHAR(512) NULL, access_token VARCHAR(255) NOT NULL, refresh_token VARCHAR(32) NOT NULL,
 client_id VARCHAR(255) NOT NULL, scopes VARCHAR(255) NULL, expires_time DATETIME NOT NULL,
 create_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
CREATE TABLE system_oauth2_refresh_token (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, user_id BIGINT NOT NULL, user_type TINYINT NOT NULL,
 refresh_token VARCHAR(32) NOT NULL, client_id VARCHAR(255) NOT NULL, scopes VARCHAR(255) NULL,
 expires_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
INSERT INTO system_users (id, tenant_id, username, password, nickname, status) VALUES (7, 1, 'admin', 'x', '管理员', 0);
INSERT INTO system_oauth2_access_token (user_id, user_type, access_token, refresh_token, client_id, expires_time, create_time, tenant_id) VALUES
 (7, 2, 'live', 'r-live', 'default', DATE_ADD(NOW(), INTERVAL 1 HOUR), '2026-09-23 10:00:00', 1),
 (7, 2, 'old', 'r-old', 'default', DATE_SUB(NOW(), INTERVAL 1 HOUR), '2026-09-22 10:00:00', 1),
 (8, 2, 'other', 'r-other', 'default', DATE_ADD(NOW(), INTERVAL 1 HOUR), '2026-09-23 11:00:00', 9);
INSERT INTO system_oauth2_refresh_token (user_id, user_type, refresh_token, client_id, expires_time, tenant_id) VALUES
 (7, 2, 'r-live', 'default', DATE_ADD(NOW(), INTERVAL 1 DAY), 1),
 (8, 2, 'r-other', 'default', DATE_ADD(NOW(), INTERVAL 1 DAY), 9);
`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Users: store, Tokens: store, Cache: &memCache{items: map[string]Token{}}}
	page, err := svc.AccessTokenPage(ctx, 1, AccessTokenQuery{ClientID: "def"})
	if err != nil || page.Total != 1 || page.List[0].AccessToken != "live" || page.List[0].CreateTime == 0 {
		t.Fatalf("%+v %v", page, err)
	}
	if err := svc.ForceLogout(ctx, 1, "other", RequestMeta{}); err != nil {
		t.Fatal(err)
	}
	var kept int
	if err := db.QueryRow(`SELECT COUNT(*) FROM system_oauth2_access_token WHERE access_token='other' AND deleted=0`).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("其他租户令牌被删：%d %v", kept, err)
	}
	if err := svc.ForceLogout(ctx, 1, "live", RequestMeta{}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM system_oauth2_access_token WHERE access_token='live' AND deleted=0`).Scan(&kept); err != nil || kept != 0 {
		t.Fatalf("本租户令牌仍在：%d %v", kept, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM system_oauth2_refresh_token WHERE refresh_token='r-live' AND deleted=0`).Scan(&kept); err != nil || kept != 0 {
		t.Fatalf("刷新令牌仍在：%d %v", kept, err)
	}
}
