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
	"golang.org/x/crypto/bcrypt"
)

func TestPermissionRequiresTenantHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	result, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	Mount(r, svc)

	req := httptest.NewRequest(http.MethodGet, "/admin-api/system/auth/get-permission-info", nil)
	req.Header.Set("Authorization", "Bearer "+result.AccessToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var body struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != codeBadRequest || !strings.Contains(body.Msg, "租户标识未传递") {
		t.Fatalf("%+v", body)
	}
}

func TestRefreshReplacesAccessToken(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	svc.Now = func() time.Time { return time.Date(2026, 9, 22, 8, 0, 0, 0, time.Local) }
	first, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.AccessToken == first.AccessToken || second.RefreshToken != first.RefreshToken {
		t.Fatalf("first %+v second %+v", first, second)
	}
	if _, err := svc.Check(context.Background(), first.AccessToken); asError(err).Code != codeUnauthorized {
		t.Fatal("旧访问令牌应失效")
	}
}

func TestMenuJSONPrefixesOrphanRootPath(t *testing.T) {
	got := menuJSON([]MenuNode{{
		Menu: Menu{ID: 12450, ParentID: 12000, Name: "统计", Path: "stat", Type: menuDir},
		Children: []MenuNode{{
			Menu: Menu{ID: 12461, ParentID: 12450, Name: "文章", Path: "article", Type: menuMenu},
		}},
	}})
	if got[0]["path"] != "/stat" {
		t.Fatal(got[0]["path"])
	}
	children := got[0]["children"].([]gin.H)
	if children[0]["path"] != "article" {
		t.Fatal(children[0]["path"])
	}
}
