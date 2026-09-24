package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func TestHealth(t *testing.T) {
	r := NewRouter("yudao-server")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body Result
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != CodeOK || body.Msg != "" {
		t.Fatalf("body %+v", body)
	}
	data, ok := body.Data.(map[string]any)
	if !ok || data["status"] != "up" || data["name"] != "yudao-server" {
		t.Fatalf("data %#v", body.Data)
	}
}

func TestCORSPreflightAllowsTenantHeader(t *testing.T) {
	r := NewRouter("yudao-server")
	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "http://localhost:80")
	req.Header.Set("Access-Control-Request-Headers", "tenant-id")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	allow := rec.Header().Get("Access-Control-Allow-Headers")
	if allow == "" || !contains(allow, "tenant-id") || !contains(allow, "Authorization") {
		t.Fatalf("allow headers %q", allow)
	}
}

func TestRecoverReturnsSystemError(t *testing.T) {
	r := NewRouter("yudao-server")
	r.GET("/boom", func(c *gin.Context) {
		panic("boom")
	})
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	var body Result
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != CodeInternal || body.Msg != "系统异常" {
		t.Fatalf("body %+v", body)
	}
}

func TestRequestIDPreserved(t *testing.T) {
	r := NewRouter("yudao-server")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-Id", "trace-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") != "trace-1" {
		t.Fatalf("request id %q", rec.Header().Get("X-Request-Id"))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && (indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
