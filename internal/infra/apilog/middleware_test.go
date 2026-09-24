package apilog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCaptureWritesAccessAndRedactsPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(Capture("yudao-server", store, func(context.Context, *gin.Context) (int64, int, int64) { return 7, 2, 1 }))
	r.POST("/admin-api/system/auth/login", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || !strings.Contains(string(body), `"password":"secret"`) {
			t.Fatalf("handler 没有收到原始请求体: %q, %v", body, err)
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "", "data": gin.H{
			"accessToken": "issued-access-token", "refreshToken": "issued-refresh-token",
		}})
	})
	req := httptest.NewRequest(http.MethodPost, "/admin-api/system/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	if store.access.UserID != 7 || store.access.ResultCode != 0 || store.tenantID != 1 {
		t.Fatalf("%+v tenant %d", store.access, store.tenantID)
	}
	if strings.Contains(store.access.RequestParams, "secret") || !strings.Contains(store.access.RequestParams, `"password":"*"`) {
		t.Fatal(store.access.RequestParams)
	}
	if !strings.Contains(rec.Body.String(), "issued-access-token") {
		t.Fatal("脱敏改变了客户端响应")
	}
	assertNoLogSecret(t, store.access, "issued-access-token", "issued-refresh-token", "secret")
	if store.access.ResponseBody != omittedAuth {
		t.Fatal(store.access.ResponseBody)
	}
}

func TestCaptureRedactsRefreshQueryAndResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(Capture("yudao-server", store, nil))
	r.POST("/admin-api/system/auth/refresh-token", func(c *gin.Context) {
		if c.Query("refreshToken") != "old-refresh-token" {
			t.Fatal("脱敏改变了 handler 收到的 query")
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "", "data": gin.H{
			"accessToken": "new-access-token", "refreshToken": "new-refresh-token",
		}})
	})
	req := httptest.NewRequest(http.MethodPost, "/admin-api/system/auth/refresh-token?refreshToken=old-refresh-token&pageNo=2", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	assertNoLogSecret(t, store.access, "old-refresh-token", "new-access-token", "new-refresh-token")
	if !strings.Contains(store.access.RequestParams, "pageNo=2") || store.access.ResponseBody != omittedAuth {
		t.Fatalf("query 或响应脱敏不正确: %+v", store.access)
	}
}

func TestCaptureRedactsNestedJSONAndForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(Capture("infra-server", store, nil))
	r.POST("/admin-api/infra/config/update", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "", "data": gin.H{
			"items": []any{gin.H{"access_token": "nested-access-token", "name": "visible"}},
		}})
	})
	req := httptest.NewRequest(http.MethodPost,
		"/admin-api/infra/config/update?access_token=query-access-token&name=visible",
		strings.NewReader(`{"clientSecret":"body-secret","items":[{"newPassword":"escaped\"secret","name":"visible"}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assertNoLogSecret(t, store.access, "query-access-token", "body-secret", "escaped", "nested-access-token")
	if !strings.Contains(store.access.ResponseBody, `"access_token":"*"`) ||
		!strings.Contains(store.access.RequestParams, `"clientSecret":"*"`) {
		t.Fatalf("JSON 脱敏不正确: %+v", store.access)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin-api/infra/config/update",
		strings.NewReader("password=form-secret&api_key=form-api-key&name=visible"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assertNoLogSecret(t, store.access, "form-secret", "form-api-key")
	if !strings.Contains(store.access.RequestParams, "name=visible") {
		t.Fatal(store.access.RequestParams)
	}
}

func TestCaptureOmitsTruncatedAndMalformedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(Capture("infra-server", store, nil))
	r.POST("/admin-api/infra/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", []byte(`{"data":{"name":"visible"}}`+strings.Repeat(" ", 2050)+`"response-token"`))
	})
	req := httptest.NewRequest(http.MethodPost, "/admin-api/infra/test?accessToken=token-in-query",
		strings.NewReader(`{"name":"`+strings.Repeat("x", 8192)+`","password":"request-token"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assertNoLogSecret(t, store.access, "token-in-query", "request-token", "response-token")
	if !strings.Contains(store.access.RequestParams, omittedRequest) || store.access.ResponseBody != omittedResponse {
		t.Fatalf("截断内容未省略: %+v", store.access)
	}
}

func TestCaptureOmitsTokenEmbeddedInMessageAndRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(Capture("system-server", store, nil))
	r.GET("/admin-api/system/example", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 401, "msg": "accessToken=message-token"})
	})
	req := httptest.NewRequest(http.MethodGet,
		"/admin-api/system/example?redirectUri=https%3A%2F%2Fexample.org%2F%3FaccessToken%3Dredirect-token", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	assertNoLogSecret(t, store.access, "message-token", "redirect-token")
	if store.access.ResultMsg != "" || store.access.ResultCode != 401 {
		t.Fatalf("结果消息或业务码不正确: %+v", store.access)
	}
}

func assertNoLogSecret(t *testing.T, log AccessLog, secrets ...string) {
	t.Helper()
	encoded, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("访问日志含敏感值 %q: %s", secret, encoded)
		}
	}
}

func TestCaptureRecordsPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		defer func() { _ = recover() }()
		c.Next()
	})
	r.Use(Capture("infra-server", store, nil))
	r.GET("/admin-api/boom", func(c *gin.Context) { panic("boom") })
	req := httptest.NewRequest(http.MethodGet, "/admin-api/boom?accessToken=panic-token", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	if store.access.ResultCode != 500 || store.errItem.ExceptionName != "panic" {
		t.Fatalf("access %+v error %+v", store.access, store.errItem)
	}
	access, _ := json.Marshal(store.access)
	errorLog, _ := json.Marshal(store.errItem)
	if strings.Contains(string(access), "panic-token") || strings.Contains(string(errorLog), "panic-token") {
		t.Fatalf("panic 日志含敏感 query: %s %s", access, errorLog)
	}
}

func TestCaptureSkipsHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{}
	r := gin.New()
	r.Use(Capture("yudao-server", store, nil))
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
	if store.access.RequestURL != "" {
		t.Fatal(store.access.RequestURL)
	}
}

// contextCheckingStore 模拟数据库驱动拒绝已取消的 Context，并记录审计预算。
type contextCheckingStore struct {
	*memStore
	accessCtxErr  error
	errorCtxErr   error
	accessBound   bool
	errorBound    bool
	errorTenantID int64
}

func (s *contextCheckingStore) CreateAccess(ctx context.Context, tenantID int64, item AccessLog) error {
	s.accessCtxErr = ctx.Err()
	_, s.accessBound = ctx.Deadline()
	if s.accessCtxErr != nil {
		return s.accessCtxErr
	}
	return s.memStore.CreateAccess(ctx, tenantID, item)
}

func (s *contextCheckingStore) CreateError(ctx context.Context, tenantID int64, item ErrorLog) error {
	s.errorCtxErr = ctx.Err()
	_, s.errorBound = ctx.Deadline()
	if s.errorCtxErr != nil {
		return s.errorCtxErr
	}
	s.errorTenantID = tenantID
	return s.memStore.CreateError(ctx, tenantID, item)
}

func TestCapturePreservesIdentityAndAccessAfterRequestCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &contextCheckingStore{memStore: &memStore{}}
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	identityCalled := false
	r := gin.New()
	r.Use(Capture("yudao-server", store, func(ctx context.Context, _ *gin.Context) (int64, int, int64) {
		identityCalled = true
		if ctx.Err() != nil {
			t.Errorf("审计身份解析收到已取消的 Context：%v", ctx.Err())
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("审计身份解析缺少超时上限")
		}
		return 7, 2, 1
	}))
	r.GET("/admin-api/system/user/get", func(c *gin.Context) {
		cancel()
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"id": 7}})
	})
	req := httptest.NewRequest(http.MethodGet, "/admin-api/system/user/get?id=7", nil).WithContext(requestCtx)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !identityCalled || store.accessCtxErr != nil || !store.accessBound ||
		store.tenantID != 1 || store.access.UserID != 7 || store.access.UserType != 2 {
		t.Fatalf("请求取消后丢失审计身份或访问日志：status=%d identity=%v ctxErr=%v bounded=%v tenant=%d access=%+v",
			recorder.Code, identityCalled, store.accessCtxErr, store.accessBound, store.tenantID, store.access)
	}
}

func TestCapturePreservesPanicLogAfterRequestCanceled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &contextCheckingStore{memStore: &memStore{}}
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		defer func() { _ = recover() }()
		c.Next()
	})
	r.Use(Capture("yudao-server", store, func(context.Context, *gin.Context) (int64, int, int64) { return 7, 2, 1 }))
	r.GET("/admin-api/boom", func(*gin.Context) {
		cancel()
		panic("boom")
	})
	req := httptest.NewRequest(http.MethodGet, "/admin-api/boom", nil).WithContext(requestCtx)
	r.ServeHTTP(httptest.NewRecorder(), req)
	if store.accessCtxErr != nil || store.errorCtxErr != nil || !store.accessBound || !store.errorBound ||
		store.tenantID != 1 || store.errorTenantID != 1 || store.access.UserID != 7 || store.errItem.UserID != 7 || store.errItem.ExceptionName != "panic" {
		t.Fatalf("请求取消后丢失 panic 审计：accessErr=%v errorErr=%v accessBound=%v errorBound=%v access=%+v error=%+v",
			store.accessCtxErr, store.errorCtxErr, store.accessBound, store.errorBound, store.access, store.errItem)
	}
}
