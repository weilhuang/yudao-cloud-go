//go:build integration

package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	redistest "github.com/testcontainers/testcontainers-go/modules/redis"

	_ "github.com/go-sql-driver/mysql"
)

// TestTokenRPCWithMySQLAndRedis 用真实表、缓存和 HTTP 路由验证 Java Feign 合约的关键状态转换。
func TestTokenRPCWithMySQLAndRedis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("rpc"),
		mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })
	redisC, err := redistest.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(context.Background()) })
	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true", "loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range strings.Split(tokenRPCSchema, ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	addr, err := redisC.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: addr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Users: store, Tokens: store, Cache: &RedisCache{Client: redisClient}}
	svc.ValidateTenant = func(_ context.Context, tenantID int64, _ time.Time) error {
		if tenantID != 1 && tenantID != 2 {
			return badRequest("租户不存在")
		}
		return nil
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, svc)
	MountRPC(r, svc)
	call := func(method, path, body, tenant string) rpcResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if tenant != "" {
			req.Header.Set("tenant-id", tenant)
		}
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var result rpcResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	create := func(scopes string) (access, refresh string) {
		t.Helper()
		body := `{"userId":7,"userType":2,"clientId":"second","scopes":` + scopes + `}`
		result := call(http.MethodPost, "/rpc-api/system/oauth2/token/create", body, "1")
		if result.Code != 0 {
			t.Fatalf("创建失败: %+v", result)
		}
		var token struct{ AccessToken, RefreshToken string }
		if err := json.Unmarshal(result.Data, &token); err != nil {
			t.Fatal(err)
		}
		return token.AccessToken, token.RefreshToken
	}

	access, refresh := create(`["user.read"]`)
	var accessScopes, refreshScopes sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT scopes FROM system_oauth2_access_token WHERE access_token=?`, access).Scan(&accessScopes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT scopes FROM system_oauth2_refresh_token WHERE refresh_token=?`, refresh).Scan(&refreshScopes); err != nil {
		t.Fatal(err)
	}
	if accessScopes.String != `["user.read"]` || refreshScopes.String != `["user.read"]` {
		t.Fatalf("授权范围未持久化: access=%+v refresh=%+v", accessScopes, refreshScopes)
	}
	if cached, err := svc.Cache.Get(ctx, access); err != nil || cached == nil || len(cached.Scopes) != 1 || cached.Scopes[0] != "user.read" {
		t.Fatalf("Redis 范围错误: %+v, %v", cached, err)
	}
	// Java 的 StringRedisTemplate 直接解析 lowerCamelCase 和毫秒时间戳。
	raw, err := redisClient.Get(ctx, tokenKey(access)).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var javaValue struct {
		AccessToken string   `json:"accessToken"`
		TenantID    int64    `json:"tenantId"`
		Scopes      []string `json:"scopes"`
		ExpiresTime int64    `json:"expiresTime"`
	}
	if err := json.Unmarshal(raw, &javaValue); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if javaValue.AccessToken != access || javaValue.TenantID != 1 || javaValue.ExpiresTime <= time.Now().UnixMilli() ||
		len(javaValue.Scopes) != 1 || javaValue.Scopes[0] != "user.read" || fields["ExpiresAt"] != nil {
		t.Fatalf("共享 Redis 值与 Java DTO 不兼容: %s", raw)
	}
	javaKey := "java-issued-token"
	if err := redisClient.Set(ctx, tokenKey(javaKey), `{"accessToken":"java-issued-token","refreshToken":"refresh","userId":7,"userType":2,"tenantId":1,"clientId":"default","scopes":["user.read"],"expiresTime":1893456000000,"userInfo":{"nickname":"管理员"}}`, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if fromJava, err := svc.Cache.Get(ctx, javaKey); err != nil || fromJava == nil || fromJava.UserID != 7 || fromJava.ExpiresAt.UnixMilli() != 1893456000000 {
		t.Fatalf("Go 无法读取 Java 缓存值: %+v, %v", fromJava, err)
	}
	if err := svc.Cache.Delete(ctx, javaKey); err != nil {
		t.Fatal(err)
	}
	if res := call(http.MethodGet, "/rpc-api/system/oauth2/token/check?accessToken="+access, "", ""); res.Code != 0 || !strings.Contains(string(res.Data), `"scopes":["user.read"]`) {
		t.Fatalf("校验 DTO 错误: %+v", res)
	}
	refreshCheck := call(http.MethodGet, "/rpc-api/system/oauth2/token/check?accessToken="+refresh, "", "")
	var refreshData struct {
		UserID      int64             `json:"userId"`
		ExpiresTime int64             `json:"expiresTime"`
		UserInfo    map[string]string `json:"userInfo"`
	}
	if refreshCheck.Code != 0 || json.Unmarshal(refreshCheck.Data, &refreshData) != nil ||
		refreshData.UserID != 7 || refreshData.ExpiresTime <= javaValue.ExpiresTime || refreshData.UserInfo["nickname"] != "管理员" {
		t.Fatalf("刷新令牌回退校验错误: %+v, %+v", refreshCheck, refreshData)
	}
	if res := call(http.MethodPut, "/rpc-api/system/oauth2/token/refresh?refreshToken="+refresh+"&clientId=default", "", "1"); res.Code != 400 {
		t.Fatalf("跨 clientId 刷新: %+v", res)
	}
	refreshed := call(http.MethodPut, "/rpc-api/system/oauth2/token/refresh?refreshToken="+refresh+"&clientId=second", "", "1")
	if refreshed.Code != 0 {
		t.Fatalf("刷新失败: %+v", refreshed)
	}
	var next struct{ AccessToken, RefreshToken string }
	if err := json.Unmarshal(refreshed.Data, &next); err != nil || next.RefreshToken != refresh || next.AccessToken == access {
		t.Fatalf("刷新结果错误: %+v, %v", next, err)
	}
	if token, err := store.FindAccess(ctx, access); err != nil || token != nil {
		t.Fatalf("旧访问令牌未删: %+v, %v", token, err)
	}
	if cached, err := svc.Cache.Get(ctx, access); err != nil || cached != nil {
		t.Fatalf("旧访问令牌缓存未删: %+v, %v", cached, err)
	}
	accessNull, _ := create("null")
	accessEmpty, _ := create("[]")
	if err := db.QueryRowContext(ctx, `SELECT scopes FROM system_oauth2_access_token WHERE access_token=?`, accessNull).Scan(&accessScopes); err != nil || accessScopes.Valid {
		t.Fatalf("null 授权范围未保存为 NULL: %+v, %v", accessScopes, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT scopes FROM system_oauth2_access_token WHERE access_token=?`, accessEmpty).Scan(&accessScopes); err != nil || accessScopes.String != "[]" {
		t.Fatalf("空数组授权范围未保存为 []: %+v, %v", accessScopes, err)
	}
	if res := call(http.MethodDelete, "/rpc-api/system/oauth2/token/remove-by-user?userId=7&userType=2", "", "2"); res.Code != 0 {
		t.Fatalf("其他租户的撤销请求失败: %+v", res)
	}
	if token, err := store.FindAccess(ctx, next.AccessToken); err != nil || token == nil {
		t.Fatalf("跨租户删除了令牌: %+v, %v", token, err)
	}
	if res := call(http.MethodDelete, "/rpc-api/system/oauth2/token/remove-by-user?userId=7&userType=2", "", "1"); res.Code != 0 || string(res.Data) != "true" {
		t.Fatalf("按用户删除失败: %+v", res)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_access_token WHERE user_id=7 AND deleted=0`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("残留访问令牌: %d, %v", remaining, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_refresh_token WHERE user_id=7 AND deleted=0`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("残留刷新令牌: %d, %v", remaining, err)
	}
	// 缓存不可用时已提交的 MySQL 令牌会补偿删除，不留可刷新的令牌。
	brokenRedis := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: 0})
	svc.Cache = &RedisCache{Client: brokenRedis}
	if _, err := svc.CreateAccessToken(ctx, 1, 7, userTypeAdmin, "second", nil); err == nil {
		t.Fatal("缓存不可用时签发竟然成功")
	}
	_ = brokenRedis.Close()
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_refresh_token WHERE user_id=7 AND deleted=0`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("缓存失败后留下刷新令牌: %d, %v", remaining, err)
	}
	// 用测试表约束让第二条 INSERT 失败：刷新令牌写入应随事务一起回滚。
	svc.Cache = &RedisCache{Client: redisClient}
	if _, err := svc.CreateAccessToken(ctx, 1, 7, userTypeAdmin, "blocked", nil); err == nil {
		t.Fatal("访问令牌约束失败时签发竟然成功")
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_oauth2_refresh_token WHERE client_id='blocked'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("事务回滚后仍有刷新令牌: %d, %v", remaining, err)
	}
	keys, err := redisClient.Keys(ctx, "oauth2_access_token:*").Result()
	if err != nil || len(keys) != 0 {
		t.Fatalf("事务失败后仍有缓存令牌: %v, %v", keys, err)
	}
}

const tokenRPCSchema = `
CREATE TABLE system_users (
 id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, username VARCHAR(30) NOT NULL,
 password VARCHAR(100) NOT NULL, nickname VARCHAR(30) NOT NULL, avatar VARCHAR(512),
 email VARCHAR(50), dept_id BIGINT, status TINYINT NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0,
 login_ip VARCHAR(50), login_date DATETIME
);
CREATE TABLE system_oauth2_client (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, client_id VARCHAR(255) NOT NULL,
 status TINYINT NOT NULL, access_token_validity_seconds INT NOT NULL,
 refresh_token_validity_seconds INT NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_oauth2_access_token (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, user_id BIGINT NOT NULL, user_type TINYINT NOT NULL,
 user_info VARCHAR(512), access_token VARCHAR(255) NOT NULL, refresh_token VARCHAR(32) NOT NULL,
 client_id VARCHAR(255) NOT NULL, scopes VARCHAR(255) NULL, expires_time DATETIME NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL,
 CONSTRAINT chk_rpc_client_not_blocked CHECK (client_id <> 'blocked')
);
CREATE TABLE system_oauth2_refresh_token (
 id BIGINT PRIMARY KEY AUTO_INCREMENT, user_id BIGINT NOT NULL, user_type TINYINT NOT NULL,
 refresh_token VARCHAR(32) NOT NULL, client_id VARCHAR(255) NOT NULL, scopes VARCHAR(255) NULL,
 expires_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
INSERT INTO system_users (id, tenant_id, username, password, nickname, status) VALUES (7, 1, 'admin', '', '管理员', 0);
INSERT INTO system_oauth2_client (client_id, status, access_token_validity_seconds, refresh_token_validity_seconds)
 VALUES ('default', 0, 1800, 86400), ('second', 0, 3600, 86400), ('blocked', 0, 3600, 86400);
`
