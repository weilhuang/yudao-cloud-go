package auth

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// Mount 挂上管理端登录接口和给其他服务用的校验接口。
// 管理端前缀是 /admin-api，对应 controller.admin。校验接口前缀是 /rpc-api，不走浏览器。
func Mount(r *gin.Engine, svc *Service) {
	admin := r.Group("/admin-api/system/auth")
	admin.POST("/login", func(c *gin.Context) { login(c, svc) })
	admin.POST("/logout", func(c *gin.Context) { logout(c, svc) })
	admin.POST("/refresh-token", func(c *gin.Context) { refresh(c, svc) })
	admin.GET("/get-permission-info", func(c *gin.Context) { permission(c, svc) })
	admin.POST("/register", func(c *gin.Context) { register(c, svc) })
	admin.POST("/sms-login", func(c *gin.Context) { smsLogin(c, svc) })
	admin.POST("/send-sms-code", func(c *gin.Context) { sendSmsCode(c, svc) })
	admin.POST("/reset-password", func(c *gin.Context) { resetPassword(c, svc) })

	tokens := r.Group("/admin-api/system/oauth2-token")
	tokens.GET("/page", func(c *gin.Context) { tokenPage(c, svc) })
	tokens.DELETE("/delete", func(c *gin.Context) { tokenDelete(c, svc) })
	tokens.DELETE("/delete-list", func(c *gin.Context) { tokenDeleteList(c, svc) })

	rpc := r.Group("/rpc-api/system/oauth2/token")
	rpc.GET("/check", func(c *gin.Context) { check(c, svc) })
}

func requestMeta(c *gin.Context) RequestMeta {
	return RequestMeta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent(), TraceID: c.GetString("requestID")}
}

func login(c *gin.Context, svc *Service) {
	var req struct {
		Username            string `json:"username"`
		Password            string `json:"password"`
		CaptchaVerification string `json:"captchaVerification"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	if req.Username == "" {
		writeErr(c, badRequest("登录账号不能为空"))
		return
	}
	if req.Password == "" {
		writeErr(c, badRequest("密码不能为空"))
		return
	}
	result, err := svc.Login(c.Request.Context(), tenantID(c), req.Username, req.Password, req.CaptchaVerification, requestMeta(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, gin.H{
		"userId":       result.UserID,
		"accessToken":  result.AccessToken,
		"refreshToken": result.RefreshToken,
		"expiresTime":  result.ExpiresTime,
	})
}

func logout(c *gin.Context, svc *Service) {
	if err := svc.Logout(c.Request.Context(), bearer(c), requestMeta(c)); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func refresh(c *gin.Context, svc *Service) {
	result, err := svc.Refresh(c.Request.Context(), c.Query("refreshToken"))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, gin.H{
		"userId":       result.UserID,
		"accessToken":  result.AccessToken,
		"refreshToken": result.RefreshToken,
		"expiresTime":  result.ExpiresTime,
	})
}

func permission(c *gin.Context, svc *Service) {
	token, err := svc.Check(c.Request.Context(), bearer(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	if token.UserType != userTypeAdmin || token.UserID <= 0 {
		writeErr(c, forbidden("您无权访问管理后台"))
		return
	}
	headerTenant, ok := headerTenant(c)
	if !ok {
		writeErr(c, badRequest("请求的租户标识未传递，请进行排查"))
		return
	}
	if headerTenant != token.TenantID {
		writeErr(c, forbidden("您无权访问该租户的数据"))
		return
	}
	info, err := svc.Permission(c.Request.Context(), token.UserID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if info == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, gin.H{
		"user": gin.H{
			"id":       info.User.ID,
			"nickname": info.User.Nickname,
			"avatar":   info.User.Avatar,
			"deptId":   info.User.DeptID,
			"username": info.User.Username,
			"email":    info.User.Email,
		},
		"roles":       info.Roles,
		"permissions": info.Permissions,
		"menus":       menuJSON(info.Menus),
	})
}

func check(c *gin.Context, svc *Service) {
	token, err := svc.Check(c.Request.Context(), c.Query("accessToken"))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, gin.H{
		"userId":      token.UserID,
		"userType":    token.UserType,
		"userInfo":    token.UserInfo,
		"tenantId":    token.TenantID,
		"scopes":      token.Scopes,
		"expiresTime": token.ExpiresAt.UnixMilli(),
	})
}

func menuJSON(nodes []MenuNode) []gin.H {
	return menuJSONLevel(nodes, true)
}

func menuJSONLevel(nodes []MenuNode, root bool) []gin.H {
	out := make([]gin.H, 0, len(nodes))
	for _, node := range nodes {
		path := node.Path
		// 父菜单缺失时，子目录会变成顶级路由。Vue Router 5 要求顶级 path 以 / 开头。
		if root && path != "" && !strings.HasPrefix(path, "/") && !strings.Contains(path, "://") {
			path = "/" + path
		}
		out = append(out, gin.H{
			"id":            node.ID,
			"parentId":      node.ParentID,
			"name":          node.Name,
			"path":          path,
			"component":     node.Component,
			"componentName": node.ComponentName,
			"icon":          node.Icon,
			"visible":       node.Visible,
			"keepAlive":     node.KeepAlive,
			"alwaysShow":    node.AlwaysShow,
			"children":      menuJSONLevel(node.Children, false),
		})
	}
	return out
}

func writeErr(c *gin.Context, err error) {
	biz := asError(err)
	httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
}

func bearer(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return ""
}

func tenantID(c *gin.Context) int64 {
	id, ok := headerTenant(c)
	if !ok {
		return 0
	}
	return id
}

func headerTenant(c *gin.Context) (int64, bool) {
	text := c.GetHeader("tenant-id")
	if text == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
