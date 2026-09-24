package ws

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestSendNoticeToAdminSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := New()
	r := gin.New()
	Mount(r, hub, func(req *http.Request) (int64, int64, int, bool) {
		return 7, 1, 2, req.URL.Query().Get("token") == "good"
	})
	server := httptest.NewServer(r)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/infra/ws?token=good"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for hub.Count(2) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	hub.SendTenant(2, 1, "notice-push", `{"id":1,"title":"hello"}`)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"type":"notice-push"`) || !strings.Contains(string(payload), "hello") {
		t.Fatal(string(payload))
	}
	bad := httptest.NewRequest(http.MethodGet, "/infra/ws?token=bad", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code)
	}
}

func TestNoticeCannotCrossTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hub := New()
	r := gin.New()
	Mount(r, hub, func(req *http.Request) (int64, int64, int, bool) {
		switch req.URL.Query().Get("token") {
		case "tenant-a":
			return 7, 1, 2, true
		case "tenant-b":
			return 8, 2, 2, true
		default:
			return 0, 0, 0, false
		}
	})
	server := httptest.NewServer(r)
	defer server.Close()
	base := "ws" + strings.TrimPrefix(server.URL, "http") + "/infra/ws?token="
	a, _, err := websocket.DefaultDialer.Dial(base+"tenant-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, _, err := websocket.DefaultDialer.Dial(base+"tenant-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	deadline := time.Now().Add(2 * time.Second)
	for hub.Count(2) != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Count(2) != 2 {
		t.Fatal("两个租户的连接未建立")
	}
	hub.SendTenant(2, 1, "notice-push", `{"title":"仅租户 A"}`)
	_ = a.SetReadDeadline(time.Now().Add(time.Second))
	_, payload, err := a.ReadMessage()
	if err != nil || !strings.Contains(string(payload), "仅租户 A") {
		t.Fatalf("租户 A 未收到公告: %s, %v", payload, err)
	}
	_ = b.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	_, payload, err = b.ReadMessage()
	var timeout net.Error
	if err == nil || !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("租户 B 收到其他租户公告或出现其他错误: %s, %v", payload, err)
	}
	if err := Publish(context.Background(), nil, 0, "notice-push", "{}"); err == nil {
		t.Fatal("缺少租户编号的跨进程公告不应发布")
	}
}
