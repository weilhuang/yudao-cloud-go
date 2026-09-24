package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

type rpcSocketFixture struct {
	router *gin.Engine
	hub    *Hub
	// 管理员 7 同时存在于两个租户；租户 1 还有管理员 8 和 App 用户 7。
	a, b, c, d *websocket.Conn
	aSession   string
	cSession   string
}

func newRPCSocketFixture(t *testing.T, check TenantCheck) rpcSocketFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := New()
	r := gin.New()
	identities := map[string]struct {
		userID, tenantID int64
		userType         int
	}{
		"a": {7, 1, 2}, "b": {8, 1, 2}, "c": {7, 2, 2}, "d": {7, 1, 1},
	}
	Mount(r, hub, func(req *http.Request) (int64, int64, int, bool) {
		user, ok := identities[req.URL.Query().Get("token")]
		return user.userID, user.tenantID, user.userType, ok
	})
	MountRPC(r, hub, check)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	connect := func(token string) *websocket.Conn {
		url := "ws" + strings.TrimPrefix(server.URL, "http") + "/infra/ws?token=" + token
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	f := rpcSocketFixture{router: r, hub: hub, a: connect("a"), b: connect("b"), c: connect("c"), d: connect("d")}
	deadline := time.Now().Add(2 * time.Second)
	for (hub.Count(2) != 3 || hub.Count(1) != 1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Count(2) != 3 || hub.Count(1) != 1 {
		t.Fatal("测试会话未全部进入 Hub")
	}
	hub.mu.Lock()
	for item := range hub.clients[2] {
		switch {
		case item.tenantID == 1 && item.userID == 7:
			f.aSession = item.id
		case item.tenantID == 2 && item.userID == 7:
			f.cSession = item.id
		}
	}
	hub.mu.Unlock()
	if f.aSession == "" || f.cSession == "" || f.aSession == f.cSession {
		t.Fatal("不同租户会话编号必须独立")
	}
	return f
}

func rpcSend(t *testing.T, r *gin.Engine, tenant string, payload string) httpx.Result {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/rpc-api/infra/websocket/send", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		req.Header.Set("tenant-id", tenant)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Feign RPC 应按 CommonResult 返回 HTTP 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	var result httpx.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertRPCFrame(t *testing.T, conn *websocket.Conn, want bool) {
	t.Helper()
	if want {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("目标会话未收到消息：%v", err)
		}
		var frame struct{ Type, Content string }
		if err := json.Unmarshal(payload, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Type != "notice-push" || frame.Content != `{"id":7}` {
			t.Fatalf("Java JsonWebSocketMessage 的 content 应是原 JSON 文本字符串：%s", payload)
		}
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, payload, err := conn.ReadMessage()
	var timeout net.Error
	if err == nil || !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("非目标会话收到消息或连接异常：%s, %v", payload, err)
	}
}

func TestWebSocketRPCSelectorsAndTenantIsolation(t *testing.T) {
	check := func(context.Context, int64, time.Time) (int, string) { return 0, "" }
	for _, tc := range []struct {
		name   string
		body   func(rpcSocketFixture) string
		wanted [4]bool
	}{
		{
			name: "指定用户同编号不能跨租户且不能跨用户类型",
			body: func(rpcSocketFixture) string {
				return `{"userType":2,"userId":7,"messageType":"notice-push","messageContent":"{\"id\":7}"}`
			},
			wanted: [4]bool{true, false, false, false},
		},
		{
			name: "用户类型广播只发本租户",
			body: func(rpcSocketFixture) string {
				return `{"userType":2,"messageType":"notice-push","messageContent":"{\"id\":7}"}`
			},
			wanted: [4]bool{true, true, false, false},
		},
		{
			name: "Session 优先于用户选择器",
			body: func(f rpcSocketFixture) string {
				return `{"sessionId":"` + f.aSession + `","userType":1,"userId":7,"messageType":"notice-push","messageContent":"{\"id\":7}"}`
			},
			wanted: [4]bool{true, false, false, false},
		},
		{
			name: "其他租户的 Session 不能借相同 userId 越界",
			body: func(f rpcSocketFixture) string {
				return `{"sessionId":"` + f.cSession + `","userType":2,"userId":7,"messageType":"notice-push","messageContent":"{\"id\":7}"}`
			},
			wanted: [4]bool{},
		},
		{
			name: "缺少目标选择器仍返回 true 但不投递",
			body: func(rpcSocketFixture) string {
				return `{"messageType":"notice-push","messageContent":"{\"id\":7}"}`
			},
			wanted: [4]bool{},
		},
		{
			name: "指定离线用户仍返回 true",
			body: func(rpcSocketFixture) string {
				return `{"userType":2,"userId":999,"messageType":"notice-push","messageContent":"{\"id\":7}"}`
			},
			wanted: [4]bool{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRPCSocketFixture(t, check)
			result := rpcSend(t, f.router, "1", tc.body(f))
			if result.Code != 0 || result.Data != true {
				t.Fatalf("Java 在匹配不到会话时也返回 success(true)：%+v", result)
			}
			for i, conn := range []*websocket.Conn{f.a, f.b, f.c, f.d} {
				assertRPCFrame(t, conn, tc.wanted[i])
			}
		})
	}
}

func TestWebSocketRPCRejectsInvalidTenantAndBodyBeforeSend(t *testing.T) {
	check := func(_ context.Context, tenantID int64, _ time.Time) (int, string) {
		if tenantID == 3 {
			return 1_001_001_006, "租户已禁用"
		}
		return 0, ""
	}
	f := newRPCSocketFixture(t, check)
	goodBody := `{"userType":2,"userId":7,"messageType":"notice-push","messageContent":"{\"id\":7}"}`
	for _, tc := range []struct {
		tenant string
		body   string
		code   int
	}{
		{"", goodBody, 400},
		{"abc", goodBody, 400},
		{"2", `{"userType":2,"userId":7,"messageType":"","messageContent":"bad"}`, 400},
		{"3", goodBody, 1_001_001_006},
		{"1", `{"userType":2,"userId":7,"messageType":"notice-push","messageContent":""}`, 400},
		{"1", `{"userType":2,"userId":7,"messageType":"notice-push","messageContent":"bad"`, 400},
	} {
		result := rpcSend(t, f.router, tc.tenant, tc.body)
		if result.Code != tc.code {
			t.Fatalf("错误请求应拒绝：tenant=%q body=%q got=%+v", tc.tenant, tc.body, result)
		}
	}
	tooLarge := `{"userType":2,"messageType":"notice-push","messageContent":"` + strings.Repeat("x", maxRPCBodyBytes) + `"}`
	if result := rpcSend(t, f.router, "1", tooLarge); result.Code != 400 {
		t.Fatalf("超大 RPC 请求体应拒绝：%+v", result)
	}
	// 如果前面的错误请求误投递，此处首先读到的就不会是合法帧。
	result := rpcSend(t, f.router, "1", goodBody)
	if result.Code != 0 || result.Data != true {
		t.Fatalf("合法请求须成功：%+v", result)
	}
	assertRPCFrame(t, f.a, true)
	for _, conn := range []*websocket.Conn{f.b, f.c, f.d} {
		assertRPCFrame(t, conn, false)
	}
}

func TestWebSocketRPCSessionIndexRemovedOnDisconnect(t *testing.T) {
	f := newRPCSocketFixture(t, func(context.Context, int64, time.Time) (int, string) { return 0, "" })
	_ = f.a.Close()
	deadline := time.Now().Add(2 * time.Second)
	for f.hub.Count(2) != 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	f.hub.mu.Lock()
	_, stillIndexed := f.hub.sessions[f.aSession]
	f.hub.mu.Unlock()
	if f.hub.Count(2) != 2 || stillIndexed {
		t.Fatal("断开的会话必须从 Hub 和 Session 索引移除")
	}
	body := `{"sessionId":"` + f.aSession + `","messageType":"notice-push","messageContent":"{\"id\":7}"}`
	if result := rpcSend(t, f.router, "1", body); result.Code != 0 || result.Data != true {
		t.Fatalf("离线 Session 的发送应返回 true：%+v", result)
	}
	for _, conn := range []*websocket.Conn{f.b, f.c, f.d} {
		assertRPCFrame(t, conn, false)
	}
}
