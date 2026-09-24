//go:build integration

package identity

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
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestSocialRPCOnMySQL(t *testing.T) {
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
CREATE TABLE system_social_client (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 name varchar(64) NOT NULL,
 social_type tinyint NOT NULL,
 user_type tinyint NOT NULL,
 client_id varchar(64) NOT NULL,
 client_secret varchar(64) NOT NULL,
 agent_id varchar(64) NULL,
 public_key varchar(255) NULL,
 status tinyint NOT NULL,
 tenant_id bigint NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0,
 create_time datetime NULL
);
INSERT INTO system_social_user (type, openid, nickname, avatar, code, state, tenant_id, deleted, create_time)
 VALUES (10, 'gitee-1', '旧昵称', 'https://img.example/a.png', 'code-1', 'state-1', 1, 0, NOW());
INSERT INTO system_social_client (name, social_type, user_type, client_id, client_secret, status, tenant_id, deleted, create_time)
 VALUES ('Gitee', 10, 1, 'gitee-app', 'secret', 0, 1, 0, NOW());
`); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := &Service{Store: &MySQL{DB: db}, StateStore: &mapState{values: map[string]string{}}}
	MountSocialRPC(r, svc, nil)

	body := callRPC(t, r, http.MethodPost, "/rpc-api/system/social-user/bind", `{"userId":7,"userType":2,"socialType":10,"code":"code-1","state":"state-1"}`)
	if body.Code != 0 || string(body.Data) != `"gitee-1"` {
		t.Fatalf("绑定应返回 openid：%s %s", body.Msg, body.Data)
	}
	got := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-user/get-by-user-id?userType=2&userId=7&socialType=10", "")
	var view socialUserDTO
	if err := json.Unmarshal(got.Data, &view); err != nil || view.OpenID != "gitee-1" || view.UserID == nil || *view.UserID != 7 || view.Nickname != "旧昵称" {
		t.Fatalf("按用户查询：%s %v", got.Data, err)
	}
	member := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-user/get-by-user-id?userType=1&userId=7&socialType=10", "")
	if string(member.Data) != "null" {
		t.Fatalf("会员类型不应看到管理员绑定：%s", member.Data)
	}
	byCode := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-user/get-by-code?userType=2&socialType=10&code=code-1&state=state-1", "")
	view = socialUserDTO{}
	if err := json.Unmarshal(byCode.Data, &view); err != nil || view.UserID == nil || *view.UserID != 7 {
		t.Fatalf("按授权码应带回已绑定用户：%s %v", byCode.Data, err)
	}
	other := callRPC(t, r, http.MethodDelete, "/rpc-api/system/social-user/unbind", `{"userId":7,"userType":1,"socialType":10,"openid":"gitee-1"}`)
	if other.Code != 0 {
		t.Fatal(other.Msg)
	}
	still := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-user/get-by-user-id?userType=2&userId=7&socialType=10", "")
	if strings.Contains(string(still.Data), "null") && !strings.Contains(string(still.Data), "gitee-1") {
		t.Fatalf("会员解绑不能清掉管理员绑定：%s", still.Data)
	}
	removed := callRPC(t, r, http.MethodDelete, "/rpc-api/system/social-user/unbind", `{"userId":7,"userType":2,"socialType":10,"openid":"gitee-1"}`)
	if removed.Code != 0 || string(removed.Data) != "true" {
		t.Fatalf("解绑：%s %s", removed.Msg, removed.Data)
	}
	after := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-user/get-by-user-id?userType=2&userId=7&socialType=10", "")
	if string(after.Data) != "null" {
		t.Fatalf("解绑后应为空：%s", after.Data)
	}
	unboundCode := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-user/get-by-code?userType=2&socialType=10&code=code-1&state=state-1", "")
	view = socialUserDTO{}
	if err := json.Unmarshal(unboundCode.Data, &view); err != nil || view.OpenID != "gitee-1" || view.UserID != nil {
		t.Fatalf("未绑定授权码的 userId 应为空：%s %v", unboundCode.Data, err)
	}
	missing := callRPC(t, r, http.MethodDelete, "/rpc-api/system/social-user/unbind", `{"userId":7,"userType":2,"socialType":10,"openid":"missing"}`)
	if missing.Code != 1_002_018_001 {
		t.Fatalf("不存在的 openid：%d %s", missing.Code, missing.Msg)
	}
	link := callRPC(t, r, http.MethodGet, "/rpc-api/system/social-client/get-authorize-url?socialType=10&userType=1&redirectUri=https%3A%2F%2Fapp.example%2Fcb", "")
	if link.Code != 0 || !strings.Contains(string(link.Data), "gitee.com/oauth/authorize") || !strings.Contains(string(link.Data), "gitee-app") {
		t.Fatalf("授权地址：%s %s", link.Msg, link.Data)
	}
}

type rpcBody struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func callRPC(t *testing.T, r http.Handler, method, path, payload string) rpcBody {
	t.Helper()
	var reader *strings.Reader
	if payload == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(payload)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("tenant-id", "1")
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var body rpcBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(rec.Body.String(), err)
	}
	return body
}
