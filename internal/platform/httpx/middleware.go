package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-Id"

// RequestID 让访问日志和后续排障能对上同一次请求。调用方已经带了就沿用。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" {
			id = newID()
		}
		c.Writer.Header().Set(requestIDHeader, id)
		c.Set("requestID", id)
		c.Next()
	}
}

// AccessLog 记录方法、路径、状态和耗时。不记录 body，避免把令牌和密码打进日志。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http",
			"requestId", c.GetString("requestID"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency", time.Since(start).String(),
		)
	}
}

// Recover 把 panic 收成统一 JSON。msg 用「系统异常」，和 Java 网关的对外文案一致。
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("http panic", "requestId", c.GetString("requestID"), "err", r)
				Fail(c, http.StatusInternalServerError, CodeInternal, "系统异常")
				c.Abort()
			}
		}()
		c.Next()
	}
}

// CORS 放行管理后台会带的头。
// 允许任意 Origin 时不能同时开 Allow-Credentials，浏览器会直接拒绝。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, tenant-id, X-Request-Id")
		h.Set("Access-Control-Expose-Headers", requestIDHeader)
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
