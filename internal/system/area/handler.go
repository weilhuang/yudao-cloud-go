package area

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上固定 Vben 地区页和管理端 IP 查询，以及应用端免登录地区树。
// 管理端没有单独的按钮权限，但必须登录且租户头与令牌一致。
// 应用端对应 AppAreaController 的 @PermitAll，不要求令牌和 tenant-id。
func Mount(r *gin.Engine, sessions *auth.Service) {
	admin := r.Group("/admin-api/system/area")
	admin.GET("/tree", func(c *gin.Context) {
		if !allowAdmin(c, sessions) {
			return
		}
		httpx.OK(c, Tree())
	})
	admin.GET("/get-by-ip", func(c *gin.Context) {
		if !allowAdmin(c, sessions) {
			return
		}
		ip, present := c.GetQuery("ip")
		if !present {
			httpx.Fail(c, http.StatusOK, 400, "请求参数缺失:ip")
			return
		}
		name, err := NameByIP(ip)
		if err != nil {
			httpx.Fail(c, http.StatusOK, 500, "系统异常")
			return
		}
		httpx.OK(c, name)
	})
	r.GET("/app-api/system/area/tree", func(c *gin.Context) {
		httpx.OK(c, Tree())
	})
}

func allowAdmin(c *gin.Context, sessions *auth.Service) bool {
	header := c.GetHeader("Authorization")
	accessToken := ""
	if strings.HasPrefix(header, "Bearer ") {
		accessToken = strings.TrimPrefix(header, "Bearer ")
	}
	_, tenantID, _, err := sessions.Session(c.Request.Context(), accessToken)
	if err != nil {
		var biz *auth.Error
		if errors.As(err, &biz) {
			httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		} else {
			httpx.Fail(c, http.StatusOK, 500, "系统异常")
		}
		return false
	}
	rawTenant := c.GetHeader("tenant-id")
	parsed, parseErr := strconv.ParseInt(rawTenant, 10, 64)
	if rawTenant == "" || parseErr != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求的租户标识未传递，请进行排查")
		return false
	}
	if parsed != tenantID {
		httpx.Fail(c, http.StatusOK, 403, "您无权访问该租户的数据")
		return false
	}
	return true
}
