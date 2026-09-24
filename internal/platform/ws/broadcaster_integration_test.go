//go:build integration

package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestRedisBroadcastRoutesAcrossHubs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := rediscontainer.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	addr, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	// 各子测试使用独立的 Hub 和订阅者，模拟同一 Redis 上的两个独立进程。
	newReplica := func(t *testing.T, identities map[string]socketIdentity) (*gin.Engine, *Hub, *Broadcaster, string) {
		t.Helper()
		client := redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { _ = client.Close() })
		hub := New()
		broadcaster, err := StartBroadcaster(ctx, client, hub)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(broadcaster.Close)
		router := gin.New()
		Mount(router, hub, func(req *http.Request) (int64, int64, int, bool) {
			identity, ok := identities[req.URL.Query().Get("token")]
			return identity.userID, identity.tenantID, identity.userType, ok
		})
		MountRPC(router, broadcaster, func(context.Context, int64, time.Time) (int, string) { return 0, "" })
		server := httptest.NewServer(router)
		t.Cleanup(server.Close)
		return router, hub, broadcaster, "ws" + strings.TrimPrefix(server.URL, "http") + "/infra/ws?token="
	}
	connect := func(t *testing.T, base, token string, hub *Hub, userType, count int) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(base+token, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		deadline := time.Now().Add(2 * time.Second)
		for hub.Count(userType) < count && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if hub.Count(userType) < count {
			t.Fatal("WebSocket 会话没有进入目标副本")
		}
		return conn
	}

	t.Run("按用户跨两个副本投递且隔离租户和用户类型", func(t *testing.T) {
		routerA, hubA, _, baseA := newReplica(t, map[string]socketIdentity{
			"a": {userID: 7, tenantID: 1, userType: 2},
		})
		_, hubB, _, baseB := newReplica(t, map[string]socketIdentity{
			"b":            {userID: 7, tenantID: 1, userType: 2},
			"other-tenant": {userID: 7, tenantID: 2, userType: 2},
			"app-user":     {userID: 7, tenantID: 1, userType: 1},
		})
		a := connect(t, baseA, "a", hubA, 2, 1)
		b := connect(t, baseB, "b", hubB, 2, 1)
		otherTenant := connect(t, baseB, "other-tenant", hubB, 2, 2)
		appUser := connect(t, baseB, "app-user", hubB, 1, 1)
		result := rpcSend(t, routerA, "1", `{"userType":2,"userId":7,"messageType":"notice-push","messageContent":"{\"id\":7}"}`)
		if result.Code != 0 || result.Data != true {
			t.Fatalf("跨实例用户投递失败: %+v", result)
		}
		assertRPCFrame(t, a, true)
		assertRPCFrame(t, b, true)
		assertRPCFrame(t, a, false)
		assertRPCFrame(t, b, false)
		assertRPCFrame(t, otherTenant, false)
		assertRPCFrame(t, appUser, false)
	})

	t.Run("公告同时到达本地和远端副本", func(t *testing.T) {
		_, hubA, broadcasterA, baseA := newReplica(t, map[string]socketIdentity{
			"local": {userID: 7, tenantID: 1, userType: 2},
		})
		_, hubB, _, baseB := newReplica(t, map[string]socketIdentity{
			"remote": {userID: 8, tenantID: 1, userType: 2},
			"other":  {userID: 9, tenantID: 2, userType: 2},
		})
		local := connect(t, baseA, "local", hubA, 2, 1)
		remote := connect(t, baseB, "remote", hubB, 2, 1)
		otherTenant := connect(t, baseB, "other", hubB, 2, 2)
		if err := Publish(ctx, broadcasterA.client, 1, "notice-push", `{"id":7}`); err != nil {
			t.Fatal(err)
		}
		assertRPCFrame(t, local, true)
		assertRPCFrame(t, remote, true)
		assertRPCFrame(t, otherTenant, false)
	})

	t.Run("广播命中远端副本但不越过租户", func(t *testing.T) {
		routerA, _, _, _ := newReplica(t, nil)
		_, hubB, _, baseB := newReplica(t, map[string]socketIdentity{
			"admin-7":  {userID: 7, tenantID: 1, userType: 2},
			"admin-8":  {userID: 8, tenantID: 1, userType: 2},
			"tenant-2": {userID: 9, tenantID: 2, userType: 2},
		})
		first := connect(t, baseB, "admin-7", hubB, 2, 1)
		second := connect(t, baseB, "admin-8", hubB, 2, 2)
		otherTenant := connect(t, baseB, "tenant-2", hubB, 2, 3)
		result := rpcSend(t, routerA, "1", `{"userType":2,"messageType":"notice-push","messageContent":"{\"id\":7}"}`)
		if result.Code != 0 || result.Data != true {
			t.Fatalf("跨实例用户类型广播失败: %+v", result)
		}
		assertRPCFrame(t, first, true)
		assertRPCFrame(t, second, true)
		assertRPCFrame(t, otherTenant, false)
	})

	t.Run("Session 选择器跨副本且优先于用户选择器", func(t *testing.T) {
		routerA, _, _, _ := newReplica(t, nil)
		_, hubB, _, baseB := newReplica(t, map[string]socketIdentity{
			"target": {userID: 7, tenantID: 1, userType: 2},
			"other":  {userID: 8, tenantID: 1, userType: 2},
		})
		target := connect(t, baseB, "target", hubB, 2, 1)
		other := connect(t, baseB, "other", hubB, 2, 2)
		hubB.mu.Lock()
		var sessionID string
		for item := range hubB.clients[2] {
			if item.userID == 7 {
				sessionID = item.id
			}
		}
		hubB.mu.Unlock()
		if sessionID == "" {
			t.Fatal("未找到远端 Session")
		}
		result := rpcSend(t, routerA, "1", `{"sessionId":"`+sessionID+`","userType":2,"userId":8,"messageType":"notice-push","messageContent":"{\"id\":7}"}`)
		if result.Code != 0 || result.Data != true {
			t.Fatalf("跨实例 Session 投递失败: %+v", result)
		}
		assertRPCFrame(t, target, true)
		assertRPCFrame(t, other, false)
	})

	t.Run("无订阅者不能伪装发布成功", func(t *testing.T) {
		router, _, broadcaster, _ := newReplica(t, nil)
		broadcaster.Close()
		result := rpcSend(t, router, "1", `{"userType":2,"messageType":"notice-push","messageContent":"{\"id\":7}"}`)
		if result.Code != 500 {
			t.Fatalf("没有 Redis 订阅者时 RPC 必须报告发布失败: %+v", result)
		}
	})

	t.Run("共用 Redis 服务的不同逻辑库不互串", func(t *testing.T) {
		_, _, db0, _ := newReplica(t, nil)
		clientDB1 := redis.NewClient(&redis.Options{Addr: addr, DB: 1})
		t.Cleanup(func() { _ = clientDB1.Close() })
		hubDB1 := New()
		broadcasterDB1, err := StartBroadcaster(ctx, clientDB1, hubDB1)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(broadcasterDB1.Close)
		router := gin.New()
		Mount(router, hubDB1, func(*http.Request) (int64, int64, int, bool) { return 7, 1, 2, true })
		server := httptest.NewServer(router)
		t.Cleanup(server.Close)
		base := "ws" + strings.TrimPrefix(server.URL, "http") + "/infra/ws?token="
		socket := connect(t, base, "user", hubDB1, 2, 1)
		if err := Publish(ctx, clientDB1, 1, "notice-push", `{"id":7}`); err != nil {
			t.Fatal(err)
		}
		assertRPCFrame(t, socket, true)
		if err := Publish(ctx, db0.client, 1, "notice-push", `{"id":7}`); err != nil {
			t.Fatal(err)
		}
		assertRPCFrame(t, socket, false)
	})
}

type socketIdentity struct {
	userID, tenantID int64
	userType         int
}
