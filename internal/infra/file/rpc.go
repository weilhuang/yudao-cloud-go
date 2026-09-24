package file

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// MountRPC 提供 Java FileApi 的两条 Feign 路由。
// Java 将 /rpc-api/infra/** 设为免登录；部署时必须由入口网络隔离 RPC 路径。
func MountRPC(r *gin.Engine, svc *Service, limits UploadLimits) {
	h := &handler{svc: svc, limits: limits.effective()}
	rpc := r.Group("/rpc-api/infra/file")
	rpc.POST("/create", h.rpcCreate)
	rpc.GET("/presigned-url", h.rpcPresignGet)
}

type rpcCreateFileRequest struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
	Type      string `json:"type"`
	// encoding/json 与 Jackson 的 byte[] JSON 形式一致：内容为 Base64 字符串。
	Content []byte `json:"content"`
}

func (h *handler) rpcCreate(c *gin.Context) {
	if c.Request.ContentLength > h.limits.MaxRequestBytes {
		writeUploadTooLarge(c)
		return
	}
	// RPC JSON 也需限制读取量。Base64 比原文件大约多三分之一。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.limits.MaxRequestBytes)
	var req rpcCreateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			writeUploadTooLarge(c)
		} else {
			httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		}
		return
	}
	if len(req.Content) == 0 {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确:文件内容不能为空")
		return
	}
	if int64(len(req.Content)) > h.limits.MaxFileBytes {
		writeUploadTooLarge(c)
		return
	}
	visitURL, err := h.svc.Upload(c.Request.Context(), req.Content, req.Name, req.Directory, req.Type)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, visitURL)
}

func (h *handler) rpcPresignGet(c *gin.Context) {
	rawURL := c.Query("url")
	if rawURL == "" {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确:URL 不能为空")
		return
	}
	var expiration *int
	if raw := c.Query("expirationSeconds"); raw != "" {
		// Java Feign 参数为 Integer，超出 32 位范围应按绑定错误处理。
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
			return
		}
		seconds := int(parsed)
		expiration = &seconds
	}
	visitURL, err := h.svc.PresignGetURL(c.Request.Context(), rawURL, expiration)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, visitURL)
}
