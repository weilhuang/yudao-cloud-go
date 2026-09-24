package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAccessTokenPageSkipsExpiredAndOtherTenant(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	svc := &Service{
		Tokens: &memTokens{access: map[string]Token{
			"live":  {ID: 2, AccessToken: "live", RefreshToken: "r2", UserID: 7, UserType: 2, TenantID: 1, ClientID: "default", ExpiresAt: now.Add(time.Hour), CreatedAt: now},
			"old":   {ID: 1, AccessToken: "old", UserID: 7, UserType: 2, TenantID: 1, ClientID: "default", ExpiresAt: now.Add(-time.Minute), CreatedAt: now},
			"other": {ID: 3, AccessToken: "other", UserID: 7, UserType: 2, TenantID: 9, ClientID: "default", ExpiresAt: now.Add(time.Hour), CreatedAt: now},
		}},
		Now: func() time.Time { return now },
	}
	page, err := svc.AccessTokenPage(context.Background(), 1, AccessTokenQuery{})
	if err != nil || page.Total != 1 || page.List[0].AccessToken != "live" {
		t.Fatalf("%+v %v", page, err)
	}
	userID := int64(8)
	page, err = svc.AccessTokenPage(context.Background(), 1, AccessTokenQuery{UserID: &userID})
	if err != nil || page.Total != 0 || len(page.List) != 0 {
		t.Fatalf("其他用户应为空：%+v %v", page, err)
	}
}

func TestForceLogoutKeepsOtherTenantAndWritesDeleteLog(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	store := &memTokens{access: map[string]Token{
		"mine":  {ID: 1, AccessToken: "mine", RefreshToken: "rm", UserID: 7, UserType: 2, TenantID: 1, ExpiresAt: now.Add(time.Hour)},
		"their": {ID: 2, AccessToken: "their", RefreshToken: "rt", UserID: 8, UserType: 2, TenantID: 9, ExpiresAt: now.Add(time.Hour)},
	}, refresh: map[string]Token{
		"rm": {RefreshToken: "rm"},
		"rt": {RefreshToken: "rt"},
	}}
	rec := &memRecorder{}
	svc := &Service{
		Users: &memUsers{user: &User{ID: 7, Username: "admin"}}, Tokens: store,
		Cache: &memCache{items: map[string]Token{"mine": {}, "their": {}}}, Recorder: rec,
	}
	if err := svc.ForceLogout(context.Background(), 1, "their", RequestMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.access["their"]; !ok || rec.last.LogType != 0 {
		t.Fatal("不能删除其他租户的令牌")
	}
	if err := svc.ForceLogout(context.Background(), 1, "mine", RequestMeta{IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.access["mine"]; ok {
		t.Fatal("本租户令牌应删除")
	}
	if _, ok := store.refresh["rm"]; ok {
		t.Fatal("对应刷新令牌应删除")
	}
	if rec.last.LogType != 202 || rec.last.Username != "admin" {
		t.Fatalf("强退日志：%+v", rec.last)
	}
	if _, err := svc.Check(context.Background(), "mine"); asError(err).Code != codeUnauthorized {
		t.Fatal("强退后不能继续通过校验")
	}
}

func TestOAuth2TokenHTTPRequiresPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	svc := testService("unused", statusEnable)
	svc.Now = func() time.Time { return now }
	store := svc.Tokens.(*memTokens)
	store.access = map[string]Token{
		"admin-token": {AccessToken: "admin-token", UserID: 7, UserType: userTypeAdmin, TenantID: 1, ExpiresAt: now.Add(time.Hour)},
		"live":        {ID: 4, AccessToken: "live", RefreshToken: "r", UserID: 7, UserType: 2, TenantID: 1, ClientID: "default", ExpiresAt: now.Add(time.Hour), CreatedAt: now},
	}
	svc.Cache.(*memCache).items["admin-token"] = store.access["admin-token"]
	r := gin.New()
	Mount(r, svc)
	call := func(method, path string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("tenant-id", "1")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var body struct {
			Code int             `json:"code"`
			Msg  string          `json:"msg"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Code, body.Msg
	}
	if code, msg := call(http.MethodGet, "/admin-api/system/oauth2-token/page"); code != 403 || msg != "没有该操作权限" {
		t.Fatalf("无权限：%d %s", code, msg)
	}
	svc.Permissions = tokenPerms{}
	if code, msg := call(http.MethodDelete, "/admin-api/system/oauth2-token/delete"); code != 400 || msg != "请求参数缺失:accessToken" {
		t.Fatalf("缺参数：%d %s", code, msg)
	}
	if code, msg := call(http.MethodDelete, "/admin-api/system/oauth2-token/delete?accessToken=live"); code != 0 || msg != "" {
		t.Fatalf("删除：%d %s", code, msg)
	}
	if _, ok := store.access["live"]; ok {
		t.Fatal("令牌仍在")
	}
}

type tokenPerms struct{ memPerms }

func (tokenPerms) MenusByRole(context.Context, []int64, bool) ([]Menu, error) {
	return []Menu{{ID: 1, Type: menuButton, Permission: "system:oauth2-token:page", Status: statusEnable},
		{ID: 2, Type: menuButton, Permission: "system:oauth2-token:delete", Status: statusEnable}}, nil
}
