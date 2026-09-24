package auth

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// MountRPC 挂载 OAuth2TokenCommonApi 尚缺的四个 Feign 端点。
// GET /check 已由 Mount 挂载，调用方须同时挂载两组路由。
func MountRPC(r *gin.Engine, svc *Service) {
	rpc := r.Group("/rpc-api/system/oauth2/token")
	rpc.POST("/create", func(c *gin.Context) { rpcCreate(c, svc) })
	rpc.DELETE("/remove", func(c *gin.Context) { rpcRemove(c, svc) })
	rpc.DELETE("/remove-by-user", func(c *gin.Context) { rpcRemoveByUser(c, svc) })
	rpc.PUT("/refresh", func(c *gin.Context) { rpcRefresh(c, svc) })
}

func rpcCreate(c *gin.Context, svc *Service) {
	tid, ok := rpcTenant(c, svc)
	if !ok {
		return
	}
	var req struct {
		UserID   *int64   `json:"userId"`
		UserType *int     `json:"userType"`
		ClientID *string  `json:"clientId"`
		Scopes   []string `json:"scopes"`
	}
	if c.ShouldBindJSON(&req) != nil || req.UserID == nil || req.UserType == nil || req.ClientID == nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	token, err := svc.CreateAccessToken(c.Request.Context(), tid, *req.UserID, *req.UserType, *req.ClientID, req.Scopes)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, rpcTokenData(token))
}

func rpcRemove(c *gin.Context, svc *Service) {
	tid, ok := rpcTenant(c, svc)
	if !ok {
		return
	}
	accessToken := c.Query("accessToken")
	if accessToken == "" {
		writeErr(c, badRequest("访问令牌不能为空"))
		return
	}
	token, err := svc.RemoveAccessToken(c.Request.Context(), tid, accessToken)
	if err != nil {
		writeErr(c, err)
		return
	}
	if token == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, rpcTokenData(token))
}

func rpcRemoveByUser(c *gin.Context, svc *Service) {
	tid, ok := rpcTenant(c, svc)
	if !ok {
		return
	}
	userID, errID := strconv.ParseInt(c.Query("userId"), 10, 64)
	userType, errType := strconv.Atoi(c.Query("userType"))
	if errID != nil || errType != nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	if err := svc.RemoveAccessTokensByUser(c.Request.Context(), tid, userID, userType); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func rpcRefresh(c *gin.Context, svc *Service) {
	tid, ok := rpcTenant(c, svc)
	if !ok {
		return
	}
	refreshToken, clientID := c.Query("refreshToken"), c.Query("clientId")
	if refreshToken == "" || clientID == "" {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	token, err := svc.RefreshTokenRPC(c.Request.Context(), refreshToken, clientID, tid)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, rpcTokenData(token))
}

func rpcTenant(c *gin.Context, svc *Service) (int64, bool) {
	tid, ok := headerTenant(c)
	if !ok {
		writeErr(c, badRequest("请求的租户标识未传递，请进行排查"))
		return 0, false
	}
	if err := svc.checkTenant(c.Request.Context(), tid); err != nil {
		writeErr(c, err)
		return 0, false
	}
	return tid, true
}

func rpcTokenData(token *Token) gin.H {
	return gin.H{
		"accessToken":  token.AccessToken,
		"refreshToken": token.RefreshToken,
		"userId":       token.UserID,
		"userType":     token.UserType,
		"expiresTime":  token.ExpiresAt.UnixMilli(),
	}
}
