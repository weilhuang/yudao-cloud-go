//go:build integration

package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/app"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/rpc"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/go-sql-driver/mysql"
)

func TestLoginAndTaggedCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
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
	redisC, err := redis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(context.Background()) })
	nacosCfg := startNacos(t, ctx)

	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true", "charset=utf8mb4", "loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	redisAddr, err := redisC.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := execSchema(dsn); err != nil {
		t.Fatal(err)
	}

	base := func(name, tag string) config.Config {
		nacos := nacosCfg
		nacos.ServiceName = "system-server"
		nacos.Tag = tag
		nacos.RegisterPort = 0
		return config.Config{
			App:     config.App{Name: name},
			HTTP:    config.HTTP{Addr: "127.0.0.1:0"},
			MySQL:   config.MySQL{DSN: dsn},
			MyBatis: config.MyBatis{EncryptorPassword: testEncryptorPassword},
			Redis:   config.Redis{Addr: redisAddr},
			Nacos:   nacos,
		}
	}
	localCtx, localCancel := context.WithCancel(ctx)
	defer localCancel()
	local, err := app.Start(localCtx, base("system-server", "local"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Shutdown(context.Background()) })
	otherCtx, otherCancel := context.WithCancel(ctx)
	defer otherCancel()
	other, err := app.Start(otherCtx, base("system-server", "other"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Shutdown(context.Background()) })

	bad := postJSON(t, local.URL()+"/admin-api/system/auth/login", `{"username":"admin","password":"wrong-pass"}`, "1")
	if bad.Code != 1002000000 {
		t.Fatalf("wrong password %+v", bad)
	}
	okBody := postJSON(t, local.URL()+"/admin-api/system/auth/login", `{"username":"admin","password":"admin123"}`, "1")
	if okBody.Code != 0 || okBody.Data["accessToken"] == "" {
		t.Fatalf("login %+v", okBody)
	}
	token := okBody.Data["accessToken"].(string)
	refreshToken := okBody.Data["refreshToken"].(string)

	noTenant := getAuth(t, local.URL()+"/admin-api/system/auth/get-permission-info", token, "")
	if noTenant.Code != 400 {
		t.Fatalf("missing tenant %+v", noTenant)
	}
	info := getAuth(t, local.URL()+"/admin-api/system/auth/get-permission-info", token, "1")
	if info.Code != 0 || info.Data["permissions"] == nil {
		t.Fatalf("permission %+v", info)
	}

	// 注册成功与发现缓存收到推送之间可能有短暂间隔；先等两个 tag 都可见。
	localPort := waitTaggedPort(t, nacosCfg, "local")
	if !strings.HasSuffix(local.URL(), ":"+u64(localPort)) {
		t.Fatalf("tag=local 选中 %d，本地服务是 %s", localPort, local.URL())
	}
	otherPort := waitTaggedPort(t, nacosCfg, "other")
	if otherPort == localPort {
		t.Fatal("两个 tag 选中了同一实例")
	}
	if _, err := rpc.Pick(nacosCfg, "system-server", "missing"); err == nil {
		t.Fatal("不存在的 tag 不应打到其他实例")
	}
	checked, err := rpc.CheckToken(ctx, nacosCfg, "local", token)
	if err != nil || checked.UserID == 0 || checked.TenantID != 1 {
		t.Fatalf("check %+v %v", checked, err)
	}
	// 对齐 Java TenantSecurityWebFilter：禁用租户后，旧令牌也不能继续访问或刷新。
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE system_tenant SET status=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	disabled := getAuth(t, local.URL()+"/admin-api/system/auth/get-permission-info", token, "1")
	if disabled.Code != 1_002_015_001 {
		t.Fatalf("已禁用租户仍可访问: %+v", disabled)
	}
	if _, err := rpc.CheckToken(ctx, nacosCfg, "local", token); err == nil {
		t.Fatal("已禁用租户的令牌仍可被 RPC 校验")
	}
	if _, err := db.ExecContext(ctx, `UPDATE system_tenant SET status=0 WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	refreshed := postForm(t, local.URL()+"/admin-api/system/auth/refresh-token?refreshToken="+url.QueryEscape(refreshToken))
	if refreshed.Code != 0 || refreshed.Data["accessToken"] == token {
		t.Fatalf("refresh %+v", refreshed)
	}
	newToken := refreshed.Data["accessToken"].(string)
	req, err := http.NewRequest(http.MethodPost, local.URL()+"/admin-api/system/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+newToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := rpc.CheckToken(ctx, nacosCfg, "local", newToken); err == nil {
		t.Fatal("注销后仍能校验令牌")
	}
}

func waitTaggedPort(t *testing.T, cfg config.Nacos, tag string) uint64 {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		instance, err := rpc.Pick(cfg, "system-server", tag)
		if err == nil {
			return instance.Port
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Nacos 发现未出现 tag=%q 的 system-server：%v", tag, lastErr)
	return 0
}

type apiBody struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data map[string]any `json:"data"`
}

func postJSON(t *testing.T, endpoint, body, tenant string) apiBody {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("tenant-id", tenant)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out apiBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postForm(t *testing.T, endpoint string) apiBody {
	t.Helper()
	resp, err := http.Post(endpoint, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out apiBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getAuth(t *testing.T, endpoint, token, tenant string) apiBody {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if tenant != "" {
		req.Header.Set("tenant-id", tenant)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out apiBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func u64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func execSchema(dsn string) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), 4)
	if err != nil {
		return err
	}
	for _, stmt := range strings.Split(authSchema, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	_, err = db.Exec(`INSERT INTO system_users (username, password, nickname, dept_id, status, deleted, tenant_id) VALUES ('admin', ?, '管理员', 10, 0, 0, 1)`, string(hash))
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO system_oauth2_client (client_id, status, access_token_validity_seconds, refresh_token_validity_seconds, deleted) VALUES ('default', 0, 1800, 2592000, 0)`)
	return err
}

const authSchema = `
CREATE TABLE system_tenant (
  id bigint PRIMARY KEY,
  name varchar(100) NOT NULL,
  contact_name varchar(100) NULL,
  contact_mobile varchar(30) NULL,
  status tinyint NOT NULL,
  websites varchar(1024) NULL,
  package_id bigint NOT NULL,
  expire_time datetime NULL,
  account_count int NOT NULL DEFAULT 0,
  create_time datetime NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_users (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  username varchar(30) NOT NULL,
  password varchar(100) NOT NULL,
  nickname varchar(30) NOT NULL,
  dept_id bigint NULL,
  avatar varchar(512) NULL,
  email varchar(50) NULL,
  status tinyint NOT NULL,
  login_ip varchar(50) NULL,
  login_date datetime NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_oauth2_client (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  client_id varchar(255) NOT NULL,
  status tinyint NOT NULL,
  access_token_validity_seconds int NOT NULL,
  refresh_token_validity_seconds int NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_oauth2_access_token (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  user_type tinyint NOT NULL,
  user_info varchar(512) NULL,
  access_token varchar(255) NOT NULL,
  refresh_token varchar(32) NOT NULL,
  client_id varchar(255) NOT NULL,
  scopes varchar(255) NULL,
  expires_time datetime NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_oauth2_refresh_token (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  refresh_token varchar(32) NOT NULL,
  user_type tinyint NOT NULL,
  client_id varchar(255) NOT NULL,
  scopes varchar(255) NULL,
  expires_time datetime NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_role (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  code varchar(100) NOT NULL,
  status tinyint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_user_role (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  user_id bigint NOT NULL,
  role_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
CREATE TABLE system_menu (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  parent_id bigint NOT NULL DEFAULT 0,
  name varchar(50) NOT NULL,
  permission varchar(100) NULL,
  type tinyint NOT NULL,
  sort int NOT NULL DEFAULT 0,
  path varchar(200) NULL,
  icon varchar(100) NULL,
  component varchar(255) NULL,
  component_name varchar(255) NULL,
  status tinyint NOT NULL,
  visible bit(1) NOT NULL DEFAULT 1,
  keep_alive bit(1) NOT NULL DEFAULT 1,
  always_show bit(1) NOT NULL DEFAULT 1,
  deleted bit(1) NOT NULL DEFAULT 0,
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE system_role_menu (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  role_id bigint NOT NULL,
  menu_id bigint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL
);
INSERT INTO system_role (id, code, status, deleted, tenant_id) VALUES (1, 'common', 0, 0, 1);
INSERT INTO system_user_role (user_id, role_id, deleted, tenant_id) VALUES (1, 1, 0, 1);
INSERT INTO system_menu (id, parent_id, name, permission, type, sort, status, visible, keep_alive, always_show, deleted) VALUES
  (1, 0, '系统', '', 1, 1, 0, 1, 1, 1, 0),
  (2, 1, '用户', '', 2, 1, 0, 1, 1, 1, 0),
  (3, 2, '查询', 'system:user:query', 3, 1, 0, 1, 1, 1, 0);
INSERT INTO system_role_menu (role_id, menu_id, deleted, tenant_id) VALUES (1, 1, 0, 1), (1, 2, 0, 1), (1, 3, 0, 1);
INSERT INTO system_tenant (id, name, status, package_id, expire_time, account_count, deleted)
  VALUES (1, '系统租户', 0, 0, '2099-01-01 00:00:00', 100, 0);
`
