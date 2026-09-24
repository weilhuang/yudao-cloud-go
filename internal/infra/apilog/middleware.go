package apilog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Identity 从访问令牌解析出用户。ctx 属于审计阶段，不受客户端断开影响。
type Identity func(ctx context.Context, c *gin.Context) (userID int64, userType int, tenantID int64)

const auditWriteTimeout = 3 * time.Second

// Capture 记录 /admin-api 和 /app-api。panic 会同时写错误日志，再把 panic 交回去。
func Capture(appName string, store Store, who Identity) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/admin-api") && !strings.HasPrefix(path, "/app-api") {
			c.Next()
			return
		}
		params := readParams(c)
		start := time.Now()
		body := &bodyCapture{ResponseWriter: c.Writer}
		c.Writer = body
		defer func() {
			rec := recover()
			// 响应已写出时客户端可能关闭连接；审计仍要在有界时间内完成。
			auditBase := context.WithoutCancel(c.Request.Context())
			auditCtx, cancelAudit := context.WithTimeout(auditBase, auditWriteTimeout)
			defer cancelAudit()
			userID, userType, tenantID := int64(0), 0, int64(0)
			if who != nil {
				userID, userType, tenantID = who(auditCtx, c)
			}
			end := time.Now()
			code, msg := body.result()
			if rec != nil {
				code, msg = 500, "系统异常"
			}
			// 认证接口的响应可能包含新签发的令牌，不能把原始响应写入访问日志。
			if sensitiveAuthPath(path) || hasEmbeddedSecret(msg) {
				msg = ""
			}
			access := AccessLog{
				TraceID: c.GetString("requestID"), UserID: userID, UserType: userType, ApplicationName: appName,
				RequestMethod: c.Request.Method, RequestURL: path, RequestParams: params, ResponseBody: safeResponse(path, body.buf.Bytes(), body.truncated),
				UserIP: c.ClientIP(), UserAgent: c.Request.UserAgent(), BeginTime: start.UnixMilli(), EndTime: end.UnixMilli(),
				Duration: int(end.Sub(start).Milliseconds()), ResultCode: code, ResultMsg: msg,
			}
			if err := store.CreateAccess(auditCtx, tenantID, access); err != nil {
				slog.Error("写入访问日志失败", "err", err, "path", path)
			}
			if rec != nil {
				// 访问日志超时不能挤占 panic 错误日志的独立写入预算。
				errorCtx, cancelError := context.WithTimeout(auditBase, auditWriteTimeout)
				defer cancelError()
				writePanic(errorCtx, c, store, appName, tenantID, userID, userType, params, rec)
				panic(rec)
			}
		}()
		c.Next()
	}
}

func writePanic(ctx context.Context, c *gin.Context, store Store, appName string, tenantID, userID int64, userType int, params string, rec any) {
	pc, file, line, _ := runtime.Caller(3)
	method := "panic"
	if fn := runtime.FuncForPC(pc); fn != nil {
		method = fn.Name()
	}
	trace := ""
	buf := make([]byte, 4096)
	if n := runtime.Stack(buf, false); n > 0 {
		trace = string(buf[:n])
	}
	message := strings.TrimSpace(strings.Split(trace, "\n")[0])
	if message == "" {
		message = "panic"
	}
	item := ErrorLog{
		TraceID: c.GetString("requestID"), UserID: userID, UserType: userType, ApplicationName: appName,
		RequestMethod: c.Request.Method, RequestURL: c.Request.URL.Path, RequestParams: params,
		UserIP: c.ClientIP(), UserAgent: c.Request.UserAgent(), ExceptionTime: time.Now().UnixMilli(),
		ExceptionName: "panic", ExceptionMessage: message, ExceptionRootCauseMessage: message, ExceptionStackTrace: trace,
		ExceptionClassName: method, ExceptionFileName: file, ExceptionMethodName: method, ExceptionLineNumber: line,
	}
	if err := store.CreateError(ctx, tenantID, item); err != nil {
		slog.Error("写入错误日志失败", "err", err)
	}
}

func readParams(c *gin.Context) string {
	query := redactValues(c.Request.URL.RawQuery)
	// 多读一字节判断是否被截断，并将读过的字节完整放回，保证 handler 读到原请求。
	prefix, _ := io.ReadAll(io.LimitReader(c.Request.Body, 8193))
	c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), c.Request.Body))
	body := safeRequestBody(c.Request.Header.Get("Content-Type"), prefix)
	if query == "" {
		return body
	}
	if body == "" {
		return query
	}
	return query + " " + body
}

type bodyCapture struct {
	gin.ResponseWriter
	status    int
	buf       bytes.Buffer
	truncated bool
}

func (b *bodyCapture) WriteHeader(code int) {
	b.status = code
	b.ResponseWriter.WriteHeader(code)
}

func (b *bodyCapture) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = 200
	}
	if b.buf.Len() < 2048 {
		remain := 2048 - b.buf.Len()
		if len(p) > remain {
			b.buf.Write(p[:remain])
			b.truncated = true
		} else {
			b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return b.ResponseWriter.Write(p)
}

func (b *bodyCapture) WriteString(s string) (int, error) { return b.Write([]byte(s)) }

func (b *bodyCapture) result() (int, string) {
	status := b.status
	if status == 0 {
		status = 200
	}
	raw := bytes.TrimSpace(b.buf.Bytes())
	if len(raw) == 0 || raw[0] != '{' {
		return status, ""
	}
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return status, ""
	}
	return payload.Code, payload.Msg
}
