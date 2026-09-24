package redismonitor

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

const monitorPermission = "infra:redis:get-monitor-info"

// Mount 只在单体和 infra 进程挂载监控页，复用管理登录态和按钮权限。
func Mount(r *gin.Engine, sessions *auth.Service, reader Reader) {
	r.GET("/admin-api/infra/redis/get-monitor-info", func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		accessToken := ""
		if strings.HasPrefix(header, "Bearer ") {
			accessToken = strings.TrimPrefix(header, "Bearer ")
		}
		_, tenantID, perms, err := sessions.Session(c.Request.Context(), accessToken)
		if err != nil {
			var biz *auth.Error
			if errors.As(err, &biz) {
				httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
			} else {
				httpx.Fail(c, http.StatusOK, httpx.CodeInternal, "系统异常")
			}
			return
		}
		tenantHeader, parseErr := strconv.ParseInt(c.GetHeader("tenant-id"), 10, 64)
		if c.GetHeader("tenant-id") == "" || parseErr != nil {
			httpx.Fail(c, http.StatusOK, 400, "请求的租户标识未传递，请进行排查")
			return
		}
		if tenantHeader != tenantID {
			httpx.Fail(c, http.StatusOK, 403, "您无权访问该租户的数据")
			return
		}
		if !perms[monitorPermission] {
			httpx.Fail(c, http.StatusOK, 403, "没有该操作权限")
			return
		}
		info, err := Read(c.Request.Context(), reader)
		if err != nil {
			// Redis 错误可能带连接信息；响应只给固定文案，不能泄露配置或凭据。
			httpx.Fail(c, http.StatusOK, httpx.CodeInternal, "系统异常")
			return
		}
		httpx.OK(c, info)
	})
}
