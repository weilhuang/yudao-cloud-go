package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSocialRPCRejectsMissingTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	MountSocialRPC(r, &Service{Store: &memStore{}}, nil)
	req := httptest.NewRequest(http.MethodGet, "/rpc-api/system/social-user/get-by-user-id?userType=2&userId=1&socialType=10", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "请求的租户标识未传递") {
		t.Fatal(rec.Body.String())
	}
}

func TestSocialRPCBindValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	MountSocialRPC(r, &Service{Store: &memStore{}}, nil)
	req := httptest.NewRequest(http.MethodPost, "/rpc-api/system/social-user/bind", strings.NewReader(`{"userType":9,"socialType":10,"code":"c","state":"s"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("tenant-id", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "用户编号不能为空") {
		t.Fatal(rec.Body.String())
	}
}

func TestSocialRPCAuthorizeURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := &memStore{social: &SocialClient{SocialType: 10, UserType: 1, ClientID: "gitee-app", Status: 0}}
	svc := &Service{Store: store, StateStore: &mapState{values: map[string]string{}}}
	MountSocialRPC(r, svc, nil)
	req := httptest.NewRequest(http.MethodGet, "/rpc-api/system/social-client/get-authorize-url?socialType=10&userType=1&redirectUri=https%3A%2F%2Fapp.example%2Fcb", nil)
	req.Header.Set("tenant-id", "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "gitee.com/oauth/authorize") || !strings.Contains(rec.Body.String(), "gitee-app") {
		t.Fatal(rec.Body.String())
	}
}

type mapState struct {
	values map[string]string
}

func (m *mapState) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.values[key] = value
	return nil
}

func (m *mapState) GetDel(_ context.Context, key string) (string, error) {
	value := m.values[key]
	delete(m.values, key)
	return value, nil
}
