// Package ws 提供 /infra/ws 会话管理与跨实例发送，消息帧是 {type, content}。
package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Identity 用查询参数里的 token 认出用户和租户。失败则拒绝连接。
type Identity func(r *http.Request) (userID, tenantID int64, userType int, ok bool)

type client struct {
	id       string
	userType int
	userID   int64
	tenantID int64
	conn     *websocket.Conn
	writeMu  sync.Mutex
}

// Hub 按用户类型保存本进程里的连接。跨进程发送由 Broadcaster 负责。
type Hub struct {
	mu       sync.Mutex
	clients  map[int]map[*client]struct{}
	sessions map[string]*client
}

func New() *Hub {
	return &Hub{clients: map[int]map[*client]struct{}{}, sessions: map[string]*client{}}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// Mount 挂上 /infra/ws。token 放在查询参数里，和前端现有连接方式一致。
func Mount(r *gin.Engine, hub *Hub, who Identity) {
	r.GET("/infra/ws", func(c *gin.Context) {
		userID, tenantID, userType, ok := who(c.Request)
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		// Java 的 WebSocketSession 有服务端会话编号。编号仅作内部路由键，
		// 不从客户端接受，避免调用方自行选择别人的会话。
		randomID := make([]byte, 16)
		if _, err := rand.Read(randomID); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		item := &client{id: hex.EncodeToString(randomID), userType: userType, userID: userID, tenantID: tenantID, conn: conn}
		hub.add(item)
		defer hub.remove(item)
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
}

func (h *Hub) add(item *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[item.userType] == nil {
		h.clients[item.userType] = map[*client]struct{}{}
	}
	h.clients[item.userType][item] = struct{}{}
	h.sessions[item.id] = item
}

func (h *Hub) Count(userType int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients[userType])
}

func (h *Hub) remove(item *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients[item.userType], item)
	delete(h.sessions, item.id)
}

// SendTenant 只发送给同租户的在线用户，避免公告跨租户泄露。
func (h *Hub) SendTenant(userType int, tenantID int64, messageType, content string) {
	h.send(tenantID, SendRequest{UserType: &userType, MessageType: messageType, MessageContent: content})
}

// Send 是单实例发送路径，供本地测试与单实例调用使用。
func (h *Hub) Send(_ context.Context, tenantID int64, req SendRequest) error {
	h.send(tenantID, req)
	return nil
}

// send 按 Java 的优先级选会话：Session、指定用户、用户类型。
// 三种方式都以 Feign 透传的租户为边界，不能把相同 userId/sessionId 的消息投给别的租户。
func (h *Hub) send(tenantID int64, req SendRequest) {
	if tenantID <= 0 {
		return
	}
	frame, err := json.Marshal(map[string]string{"type": req.MessageType, "content": req.MessageContent})
	if err != nil {
		return
	}
	h.mu.Lock()
	list := make([]*client, 0)
	if req.SessionID != "" {
		if item := h.sessions[req.SessionID]; item != nil && item.tenantID == tenantID {
			list = append(list, item)
		}
	} else if req.UserType != nil {
		for item := range h.clients[*req.UserType] {
			if item.tenantID == tenantID && (req.UserID == nil || item.userID == *req.UserID) {
				list = append(list, item)
			}
		}
	}
	h.mu.Unlock()
	for _, item := range list {
		item.writeMu.Lock()
		if err := item.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			slog.Error("发送 WebSocket 失败", "err", err, "userId", item.userID)
		}
		item.writeMu.Unlock()
	}
}
