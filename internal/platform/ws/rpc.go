package ws

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

const maxRPCBodyBytes = 2 << 20

// SendRequest 与 Java WebSocketSendReqDTO 的 JSON 字段保持一致。
// UserType 和 UserID 用指针区分未传与数值零；SessionID 的非空值优先。
type SendRequest struct {
	SessionID      string `json:"sessionId"`
	UserID         *int64 `json:"userId"`
	UserType       *int   `json:"userType"`
	MessageType    string `json:"messageType"`
	MessageContent string `json:"messageContent"`
}

// TenantCheck 返回 Java CommonResult 的错误码和消息；0 表示租户有效。
// 调用方负责把租户不存在、禁用、到期及内部错误映射成相应结果。
type TenantCheck func(context.Context, int64, time.Time) (code int, msg string)

// Sender 由本地 Hub 或跨实例 Broadcaster 实现，RPC 不关心具体传输方式。
type Sender interface {
	Send(context.Context, int64, SendRequest) error
}

// MountRPC 挂载 Java WebSocketSenderApi.send 的 Feign 地址。
// Java RPC 免登录；部署必须仅在可信内部网络暴露该路径，tenant-id 不是调用方认证。
func MountRPC(r *gin.Engine, sender Sender, check TenantCheck) {
	r.POST("/rpc-api/infra/websocket/send", func(c *gin.Context) {
		tenantID, err := strconv.ParseInt(c.GetHeader("tenant-id"), 10, 64)
		if err != nil || tenantID <= 0 {
			httpx.Fail(c, http.StatusOK, 400, "请求的租户标识未传递，请进行排查")
			return
		}
		if sender == nil || check == nil {
			httpx.Fail(c, http.StatusOK, 500, "系统异常")
			return
		}
		if code, msg := check(c.Request.Context(), tenantID, time.Now()); code != 0 {
			httpx.Fail(c, http.StatusOK, code, msg)
			return
		}
		// 请求体可以包含 JSON 字符串内容，但不能无限占用内存。
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRPCBodyBytes)
		var req SendRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.MessageType == "" || req.MessageContent == "" {
			httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
			return
		}
		// 无匹配会话仍返回 true；传输层失败则不能伪装为投递成功。
		if err := sender.Send(c.Request.Context(), tenantID, req); err != nil {
			httpx.Fail(c, http.StatusOK, 500, "WebSocket 消息发布失败")
			return
		}
		httpx.OK(c, true)
	})
}
