package sendrpc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
	"github.com/weilhuang/yudao-cloud-go/internal/system/message"
)

func TestSendRPCRoutesRejectMissingTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, &auth.Service{}, &message.Service{}, nil, nil)
	body := strings.NewReader(`{"templateCode":"USER_SEND","userId":1}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc-api/system/notify/send/send-single-admin", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "请求的租户标识未传递") {
		t.Fatal(rec.Body.String())
	}
}

func TestCompactMails(t *testing.T) {
	got := compact([]string{" a@b.c ", "", "a@b.c", "d@e.f"})
	if len(got) != 2 || got[0] != "a@b.c" || got[1] != "d@e.f" {
		t.Fatal(got)
	}
}
