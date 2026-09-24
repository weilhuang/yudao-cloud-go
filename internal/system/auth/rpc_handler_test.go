package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type rpcResult struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func TestTokenRPCRoutesAndClientIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := testService("unused", statusEnable)
	svc.Now = func() time.Time { return time.Date(2026, 9, 23, 10, 0, 0, 0, time.Local) }
	svc.Tokens.(*memTokens).clients = map[string]*Client{
		"default": {ClientID: "default", Status: statusEnable, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour},
		"second":  {ClientID: "second", Status: statusEnable, AccessTTL: 2 * time.Hour, RefreshTTL: 48 * time.Hour},
	}
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
			t.Fatalf("%s %s HTTP %d: %s", method, path, w.Code, w.Body.String())
		}
		var result rpcResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	createBody := `{"userId":7,"userType":2,"clientId":"second","scopes":["user.read"]}`
	if got := call(http.MethodPost, "/rpc-api/system/oauth2/token/create", createBody, ""); got.Code != 400 {
		t.Fatalf("未带租户仍能创建令牌: %+v", got)
	}
	created := call(http.MethodPost, "/rpc-api/system/oauth2/token/create", createBody, "1")
	if created.Code != 0 {
		t.Fatalf("create: %+v", created)
	}
	var token struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		UserID       int64  `json:"userId"`
		UserType     int    `json:"userType"`
		ExpiresTime  int64  `json:"expiresTime"`
	}
	if err := json.Unmarshal(created.Data, &token); err != nil {
		t.Fatal(err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.UserID != 7 || token.UserType != 2 || token.ExpiresTime != svc.now().Add(2*time.Hour).UnixMilli() {
		t.Fatalf("创建响应不符合 Java DTO: %+v", token)
	}
	checked := call(http.MethodGet, "/rpc-api/system/oauth2/token/check?accessToken="+token.AccessToken, "", "")
	if checked.Code != 0 || !strings.Contains(string(checked.Data), `"scopes":["user.read"]`) {
		t.Fatalf("scopes 未保留到校验接口: %+v", checked)
	}
	wrong := call(http.MethodPut, "/rpc-api/system/oauth2/token/refresh?refreshToken="+token.RefreshToken+"&clientId=default", "", "1")
	if wrong.Code != 400 || wrong.Msg != "刷新令牌的客户端编号不正确" {
		t.Fatalf("跨客户端刷新未拒绝: %+v", wrong)
	}
	if got := call(http.MethodPut, "/rpc-api/system/oauth2/token/refresh?refreshToken="+token.RefreshToken+"&clientId=second", "", "2"); got.Code != 400 {
		t.Fatalf("跨租户刷新未拒绝: %+v", got)
	}
	refreshed := call(http.MethodPut, "/rpc-api/system/oauth2/token/refresh?refreshToken="+token.RefreshToken+"&clientId=second", "", "1")
	if refreshed.Code != 0 {
		t.Fatalf("refresh: %+v", refreshed)
	}
	var next struct{ AccessToken, RefreshToken string }
	if err := json.Unmarshal(refreshed.Data, &next); err != nil {
		t.Fatal(err)
	}
	if next.AccessToken == token.AccessToken || next.RefreshToken != token.RefreshToken {
		t.Fatalf("刷新结果不正确: old=%+v next=%+v", token, next)
	}
	if got := call(http.MethodGet, "/rpc-api/system/oauth2/token/check?accessToken="+token.AccessToken, "", ""); got.Code != 401 {
		t.Fatalf("旧令牌未撤销: %+v", got)
	}
	if got := call(http.MethodDelete, "/rpc-api/system/oauth2/token/remove?accessToken="+next.AccessToken, "", "2"); got.Code != 403 {
		t.Fatalf("跨租户移除未拒绝: %+v", got)
	}
	removed := call(http.MethodDelete, "/rpc-api/system/oauth2/token/remove?accessToken="+next.AccessToken, "", "1")
	if removed.Code != 0 || !strings.Contains(string(removed.Data), next.AccessToken) {
		t.Fatalf("remove: %+v", removed)
	}
	if got := call(http.MethodDelete, "/rpc-api/system/oauth2/token/remove?accessToken="+next.AccessToken, "", "1"); got.Code != 0 || string(got.Data) != "null" {
		t.Fatalf("重复移除应返回 null: %+v", got)
	}
	if got := call(http.MethodPut, "/rpc-api/system/oauth2/token/refresh?refreshToken="+token.RefreshToken+"&clientId=second", "", "1"); got.Code != 400 {
		t.Fatalf("已移除刷新令牌仍能使用: %+v", got)
	}
}

func TestTokenRPCRemoveByUserAndMemberCannotUseAdminRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := testService("unused", statusEnable)
	r := gin.New()
	Mount(r, svc)
	MountRPC(r, svc)
	for _, userType := range []int{userTypeAdmin, userTypeMember} {
		for i := 0; i < 2; i++ {
			if _, err := svc.CreateAccessToken(context.Background(), 1, 7, userType, "default", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	store := svc.Tokens.(*memTokens)
	var memberToken string
	for _, token := range store.access {
		if token.UserType == userTypeMember {
			memberToken = token.AccessToken
			break
		}
	}
	if _, _, _, err := svc.Session(context.Background(), memberToken); asError(err).Code != 403 {
		t.Fatalf("会员令牌取得管理端会话: %v", err)
	}
	if _, err := svc.Check(context.Background(), memberToken); err != nil {
		t.Fatalf("会员令牌应可通过 RPC 校验: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin-api/system/auth/get-permission-info", nil)
	request.Header.Set("Authorization", "Bearer "+memberToken)
	request.Header.Set("tenant-id", "1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	var blocked rpcResult
	if err := json.Unmarshal(w.Body.Bytes(), &blocked); err != nil || blocked.Code != 403 {
		t.Fatalf("会员令牌进入管理端: %+v, %v", blocked, err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/rpc-api/system/oauth2/token/remove-by-user?userId=7&userType=2", nil)
	req.Header.Set("tenant-id", "1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var result rpcResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 0 || string(result.Data) != "true" {
		t.Fatalf("remove-by-user: %+v, %v", result, err)
	}
	adminCount, memberCount := 0, 0
	for _, token := range store.access {
		if token.UserType == userTypeAdmin {
			adminCount++
		} else {
			memberCount++
		}
	}
	if adminCount != 0 || memberCount != 2 {
		t.Fatalf("按类型撤销错误: admin=%d member=%d", adminCount, memberCount)
	}
}

// Java client_credentials 会以 ADMIN 类型、userId=0 签发服务令牌。
func TestTokenRPCClientCredentialsUserZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := testService("unused", statusEnable)
	r := gin.New()
	Mount(r, svc)
	MountRPC(r, svc)
	request := httptest.NewRequest(http.MethodPost, "/rpc-api/system/oauth2/token/create",
		strings.NewReader(`{"userId":0,"userType":2,"clientId":"default","scopes":["service.read"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("tenant-id", "1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	var created rpcResult
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Code != 0 {
		t.Fatalf("client_credentials 签发失败: %+v, %v", created, err)
	}
	var token struct {
		AccessToken, RefreshToken string
		UserID                    int64 `json:"userId"`
	}
	if err := json.Unmarshal(created.Data, &token); err != nil || token.UserID != 0 || token.AccessToken == "" {
		t.Fatalf("服务令牌数据错误: %+v, %v", token, err)
	}
	checked, err := svc.Check(context.Background(), token.AccessToken)
	if err != nil || checked.UserID != 0 || checked.UserType != userTypeAdmin || len(checked.UserInfo) != 0 {
		t.Fatalf("服务令牌无法通过 RPC 校验: %+v, %v", checked, err)
	}
	if _, _, _, err := svc.Session(context.Background(), token.AccessToken); asError(err).Code != 403 {
		t.Fatalf("服务令牌进入管理端 Session: %v", err)
	}
	refresh := httptest.NewRequest(http.MethodPut,
		"/rpc-api/system/oauth2/token/refresh?refreshToken="+token.RefreshToken+"&clientId=default", nil)
	refresh.Header.Set("tenant-id", "1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, refresh)
	var renewed rpcResult
	if err := json.Unmarshal(w.Body.Bytes(), &renewed); err != nil || renewed.Code != 0 {
		t.Fatalf("服务令牌刷新失败: %+v, %v", renewed, err)
	}
}
