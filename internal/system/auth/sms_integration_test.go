//go:build integration

package auth

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/go-sql-driver/mysql"
)

func TestSmsLoginRegisterAndResetOnMySQL(t *testing.T) {
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
	if _, err := db.Exec(smsAuthSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	sender := &recordingSender{}
	svc := &Service{Users: store, Tokens: store, Sms: store, Codes: sender, Cache: &memCache{items: map[string]Token{}}, CodeBegin: 9999, CodeEnd: 9999}
	if err := svc.SendSmsCode(ctx, 1, "15601691300", sceneLogin, "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if sender.last != "9999" {
		t.Fatalf("测试验证码应固定为 9999，实际 %s", sender.last)
	}
	result, err := svc.SmsLogin(ctx, 1, "15601691300", "9999", "127.0.0.1", RequestMeta{IP: "127.0.0.1"})
	if err != nil || result.AccessToken == "" {
		t.Fatal(err)
	}
	if _, err := svc.SmsLogin(ctx, 1, "15601691300", "9999", "127.0.0.1", RequestMeta{}); err == nil {
		t.Fatal("验证码不应重复使用")
	}
	registered, err := svc.Register(ctx, 1, "newuser", "新用户", "admin123", "", RequestMeta{IP: "127.0.0.1"})
	if err != nil || registered.UserID == 0 {
		t.Fatal(err, registered)
	}
	var hash string
	if err := db.QueryRowContext(ctx, `SELECT password FROM system_users WHERE username='newuser' AND tenant_id=1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin123")) != nil {
		t.Fatal("注册密码无法校验")
	}
	if err := svc.SendSmsCode(ctx, 1, "15601691300", sceneReset, "", "127.0.0.1"); err == nil {
		t.Fatal("一分钟内不应再次发送")
	}
}

func TestConcurrentSmsCodeUseAllowsOneWinner(t *testing.T) {
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
	if _, err := db.Exec(smsAuthSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	if err := store.InsertSmsCode(ctx, SmsCode{Mobile: "15601691300", Code: "135790", Scene: sceneLogin, TodayIndex: 1, CreateTime: time.Now(), TenantID: 1}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	last, err := store.LastSmsCode(ctx, "15601691300", "135790", sceneLogin, true, true)
	if err != nil || last == nil {
		t.Fatal(err)
	}
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.UseSmsCode(ctx, last.ID, "127.0.0.1", time.Now()); err == nil {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("并发使用应只有一次成功，实际 %d", wins)
	}
}

const smsAuthSchema = `
CREATE TABLE system_users (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, tenant_id BIGINT NOT NULL, username VARCHAR(30) NOT NULL,
 password VARCHAR(100) NOT NULL, nickname VARCHAR(30) NOT NULL, mobile VARCHAR(11) NULL,
 avatar VARCHAR(512) NULL, email VARCHAR(50) NULL, dept_id BIGINT NULL, post_ids VARCHAR(255) NULL,
 status TINYINT NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0, login_ip VARCHAR(50) NULL, login_date DATETIME NULL,
 create_time DATETIME NULL
);
CREATE TABLE system_tenant (
 id BIGINT PRIMARY KEY, name VARCHAR(30) NOT NULL, account_count INT NOT NULL, status TINYINT NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0
);
CREATE TABLE infra_config (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, config_key VARCHAR(100) NOT NULL, value VARCHAR(100) NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_sms_code (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, mobile VARCHAR(11) NOT NULL, code VARCHAR(6) NOT NULL,
 scene INT NOT NULL, create_ip VARCHAR(50) NOT NULL, today_index INT NOT NULL, used BIT(1) NOT NULL,
 used_time DATETIME NULL, used_ip VARCHAR(50) NULL, create_time DATETIME NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
CREATE TABLE system_oauth2_client (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, client_id VARCHAR(255) NOT NULL, status TINYINT NOT NULL,
 access_token_validity_seconds INT NOT NULL, refresh_token_validity_seconds INT NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_oauth2_access_token (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, user_id BIGINT NOT NULL, user_type TINYINT NOT NULL,
 user_info VARCHAR(512) NULL, access_token VARCHAR(255) NOT NULL, refresh_token VARCHAR(32) NOT NULL,
 client_id VARCHAR(255) NOT NULL, scopes VARCHAR(255) NULL, expires_time DATETIME NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
CREATE TABLE system_oauth2_refresh_token (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, user_id BIGINT NOT NULL, user_type TINYINT NOT NULL,
 refresh_token VARCHAR(32) NOT NULL, client_id VARCHAR(255) NOT NULL, scopes VARCHAR(255) NULL,
 expires_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
INSERT INTO system_users (id, tenant_id, username, password, nickname, mobile, status)
 VALUES (7, 1, 'admin', 'unused', '管理员', '15601691300', 0);
INSERT INTO system_tenant (id, name, account_count, status) VALUES (1, '芋道', 10, 0);
INSERT INTO infra_config (config_key, value) VALUES ('system.user.register-enabled', 'true');
INSERT INTO system_oauth2_client (client_id, status, access_token_validity_seconds, refresh_token_validity_seconds)
 VALUES ('default', 0, 1800, 86400);
`
