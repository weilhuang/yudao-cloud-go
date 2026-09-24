package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisChannelPrefix = "yudao:websocket"

// Redis 的 Pub/Sub 频道不随 SELECT 切换库，频道名必须显式包含逻辑库号。
func redisChannel(client *redis.Client) string {
	return fmt.Sprintf("%s:db:%d", redisChannelPrefix, client.Options().DB)
}

// routeMessage 同时携带租户与 Java WebSocketSendReqDTO 的全部选择器。
// 每个副本收到同一条消息后，只在自己的 Hub 中寻找匹配的连接。
type routeMessage struct {
	TenantID int64 `json:"tenantId"`
	SendRequest
}

// Broadcaster 把发送请求广播到所有 WebSocket 副本，再由各副本本地投递。
// Redis Pub/Sub 不保存离线消息；它只覆盖订阅在线期间的即时消息。
type Broadcaster struct {
	client    *redis.Client
	hub       *Hub
	sub       *redis.PubSub
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// StartBroadcaster 等到 Redis 确认订阅后才返回，避免 HTTP 已接客但本副本还收不到广播。
func StartBroadcaster(ctx context.Context, client *redis.Client, hub *Hub) (*Broadcaster, error) {
	if client == nil || hub == nil {
		return nil, errors.New("WebSocket 广播缺少 Redis 或 Hub")
	}
	listenCtx, cancel := context.WithCancel(ctx)
	readyCtx, readyCancel := context.WithTimeout(listenCtx, 5*time.Second)
	defer readyCancel()
	channel := redisChannel(client)
	sub := client.Subscribe(readyCtx, channel)
	ready, err := sub.Receive(readyCtx)
	if err != nil {
		cancel()
		_ = sub.Close()
		return nil, fmt.Errorf("WebSocket 订阅 Redis 失败: %w", err)
	}
	ack, ok := ready.(*redis.Subscription)
	if !ok || ack.Kind != "subscribe" || ack.Channel != channel {
		cancel()
		_ = sub.Close()
		return nil, fmt.Errorf("WebSocket 订阅 Redis 未确认: %T", ready)
	}
	b := &Broadcaster{client: client, hub: hub, sub: sub, cancel: cancel, done: make(chan struct{})}
	go b.listen(listenCtx)
	return b, nil
}

// Send 由 Feign RPC 使用；Redis 发送失败或当前没有订阅者时，调用方收到错误。
func (b *Broadcaster) Send(ctx context.Context, tenantID int64, req SendRequest) error {
	if b == nil {
		return errors.New("WebSocket 广播未启动")
	}
	return publishRequest(ctx, b.client, tenantID, req)
}

// Publish 向所有副本发送同租户管理员公告，包括发布者自身的 WebSocket 连接。
func Publish(ctx context.Context, client *redis.Client, tenantID int64, messageType, content string) error {
	userType := 2
	return publishRequest(ctx, client, tenantID, SendRequest{
		UserType: &userType, MessageType: messageType, MessageContent: content,
	})
}

func publishRequest(ctx context.Context, client *redis.Client, tenantID int64, req SendRequest) error {
	if tenantID <= 0 {
		return errors.New("WebSocket 消息缺少租户编号")
	}
	if client == nil {
		return errors.New("WebSocket 消息缺少 Redis")
	}
	if req.MessageType == "" || req.MessageContent == "" {
		return errors.New("WebSocket 消息类型或内容为空")
	}
	raw, err := json.Marshal(routeMessage{TenantID: tenantID, SendRequest: req})
	if err != nil {
		return err
	}
	count, err := client.Publish(ctx, redisChannel(client), raw).Result()
	if err != nil {
		return fmt.Errorf("发布 WebSocket 消息失败: %w", err)
	}
	if count == 0 {
		return errors.New("没有 WebSocket Redis 订阅者")
	}
	return nil
}

func (b *Broadcaster) listen(ctx context.Context) {
	defer close(b.done)
	for {
		message, err := b.sub.ReceiveMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// go-redis 会在连接损坏时重新订阅；继续接收，但断线期间的 Pub/Sub 消息不会重放。
			slog.Error("WebSocket Redis 订阅中断，等待重连", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		var routed routeMessage
		if err := json.Unmarshal([]byte(message.Payload), &routed); err != nil ||
			routed.TenantID <= 0 || routed.MessageType == "" || routed.MessageContent == "" {
			slog.Warn("跳过无效的 WebSocket Redis 消息", "err", err)
			continue
		}
		b.hub.send(routed.TenantID, routed.SendRequest)
	}
}

// Close 先取消订阅再等待监听循环退出，防止 Redis 客户端关闭后留下接收协程。
func (b *Broadcaster) Close() {
	if b == nil {
		return
	}
	b.closeOnce.Do(func() {
		b.cancel()
		_ = b.sub.Close()
		<-b.done
	})
}
