//go:build integration

package identity

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestAuthorizeCodeOnMySQL(t *testing.T) {
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
CREATE TABLE system_oauth2_client (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 client_id varchar(255) NOT NULL,
 secret varchar(255) NOT NULL,
 name varchar(255) NOT NULL,
 logo varchar(255) NULL,
 description varchar(255) NULL,
 status tinyint NOT NULL,
 access_token_validity_seconds int NOT NULL,
 refresh_token_validity_seconds int NOT NULL,
 redirect_uris varchar(1024) NULL,
 authorized_grant_types varchar(1024) NULL,
 scopes varchar(1024) NULL,
 auto_approve_scopes varchar(1024) NULL,
 authorities varchar(255) NULL,
 resource_ids varchar(255) NULL,
 additional_information varchar(1024) NULL,
 deleted bit(1) NOT NULL DEFAULT 0,
 create_time datetime NULL
);
CREATE TABLE system_oauth2_approve (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 user_id bigint NOT NULL,
 user_type tinyint NOT NULL,
 client_id varchar(255) NOT NULL,
 scope varchar(255) NOT NULL,
 approved bit(1) NOT NULL,
 expires_time datetime NOT NULL,
 tenant_id bigint NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0,
 create_time datetime NULL
);
CREATE TABLE system_oauth2_code (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 user_id bigint NOT NULL,
 user_type tinyint NOT NULL,
 code varchar(32) NOT NULL,
 client_id varchar(255) NOT NULL,
 scopes varchar(255) NULL,
 expires_time datetime NOT NULL,
 redirect_uri varchar(255) NULL,
 state varchar(255) NOT NULL,
 tenant_id bigint NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0,
 create_time datetime NULL
);
INSERT INTO system_oauth2_client (client_id, secret, name, logo, status, access_token_validity_seconds, refresh_token_validity_seconds,
 redirect_uris, authorized_grant_types, scopes, auto_approve_scopes, deleted)
 VALUES ('default', 'secret', '芋道', 'logo.png', 0, 1800, 86400,
 '["https://app.example/cb"]', '["authorization_code"]', '["user.read"]', '["user.read"]', 0);
`); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: &MySQL{DB: db}}
	info, err := svc.AuthorizeInfo(ctx, 1, 7, "default")
	if err != nil || info.Client.Name != "芋道" || len(info.Scopes) != 1 || info.Scopes[0].Key != "user.read" || info.Scopes[0].Value {
		t.Fatalf("%+v %v", info, err)
	}
	link, err := svc.Approve(ctx, 1, 7, "code", "default", "https://app.example/cb", "xyz", true, []ScopeChoice{{Key: "user.read", Value: true}})
	if err != nil || !strings.HasPrefix(link, "https://app.example/cb?code=") || !strings.Contains(link, "state=xyz") {
		t.Fatalf("%s %v", link, err)
	}
	code := strings.TrimPrefix(strings.Split(link, "&")[0], "https://app.example/cb?code=")
	svc.Grants = OpenGrants{Issue: func(context.Context, int64, int64, int, string, []string) (OpenToken, error) {
		return OpenToken{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user.read"}}, nil
	}}
	token, err := svc.IssueToken(ctx, TokenRequest{
		GrantType: "authorization_code", Code: code, RedirectURI: "https://app.example/cb", State: "xyz",
		ClientID: "default", Secret: "secret",
	})
	if err != nil || token.AccessToken != "access" {
		t.Fatalf("%+v %v", token, err)
	}
	if _, err := svc.IssueToken(ctx, TokenRequest{
		GrantType: "authorization_code", Code: code, RedirectURI: "https://app.example/cb", State: "xyz",
		ClientID: "default", Secret: "secret",
	}); err == nil {
		t.Fatal("授权码不能使用第二次")
	}
	var approved []byte
	if err := db.QueryRow(`SELECT approved FROM system_oauth2_approve WHERE user_id=7 AND scope='user.read'`).Scan(&approved); err != nil || len(approved) == 0 || approved[0] != 1 {
		t.Fatalf("批准记录：%v %v", approved, err)
	}
}
