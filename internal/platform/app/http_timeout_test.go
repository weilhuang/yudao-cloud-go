package app

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/ws"
)

func TestHTTPReadTimeoutStopsSlowRequestBody(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "请求体读取超时", http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	server.Config = newHTTPServer(server.Config.Handler, config.HTTP{ReadTimeoutSeconds: 1})
	server.Start()
	defer server.Close()

	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "POST /upload HTTP/1.1\r\nHost: local\r\nContent-Length: 10\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("慢请求未在读取期限内结束: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("慢请求应超时，实际 HTTP %d", response.StatusCode)
	}
}

func TestHTTPReadTimeoutDoesNotCloseUpgradedWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	hub := ws.New()
	ws.Mount(router, hub, func(*http.Request) (int64, int64, int, bool) { return 1, 1, 2, true })
	server := httptest.NewUnstartedServer(router)
	server.Config = newHTTPServer(router, config.HTTP{ReadTimeoutSeconds: 1})
	server.Start()
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/infra/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// 已升级的连接超过 HTTP 读取期限后仍可收到公告帧。
	time.Sleep(1200 * time.Millisecond)
	if hub.Count(2) != 1 {
		t.Fatal("WebSocket 在 HTTP 读取期限后被错误关闭")
	}
	hub.SendTenant(2, 1, "notice-push", "ok")
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(data), "notice-push") {
		t.Fatalf("WebSocket 未收到公告: %s, %v", data, err)
	}
}
