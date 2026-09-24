package identity

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/directory"
)

// 与 Java UserTypeEnum、SocialTypeEnum 一致。查询参数不走 @InEnum，请求体才校验范围。
var (
	rpcUserTypes   = map[int]struct{}{1: {}, 2: {}}
	rpcSocialTypes = map[int]struct{}{10: {}, 20: {}, 30: {}, 31: {}, 32: {}, 34: {}, 40: {}}
)

const (
	userTypeRangeMsg   = "必须在指定范围 [1, 2]"
	socialTypeRangeMsg = "必须在指定范围 [10, 20, 30, 31, 32, 34, 40]"
)

// MountSocialRPC 挂上社交用户和社交客户端的 Feign。
// 微信接口会访问 api.weixin.qq.com；没有启用的客户端配置时返回社交客户端不存在。
func MountSocialRPC(r *gin.Engine, svc *Service, validateTenant func(context.Context, int64, time.Time) error) {
	h := &socialRPC{svc: svc, validateTenant: validateTenant}
	user := r.Group("/rpc-api/system/social-user")
	user.POST("/bind", h.bind)
	user.DELETE("/unbind", h.unbind)
	user.GET("/get-by-user-id", h.byUser)
	user.GET("/get-by-code", h.byCode)
	client := r.Group("/rpc-api/system/social-client")
	client.GET("/get-authorize-url", h.authorizeURL)
	mountWxRPC(r, h)
}

type socialRPC struct {
	svc            *Service
	validateTenant func(context.Context, int64, time.Time) error
}

type socialUserDTO struct {
	OpenID   string `json:"openid"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	UserID   *int64 `json:"userId"`
}

func (h *socialRPC) tenant(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.GetHeader("tenant-id"), 10, 64)
	if err != nil || id <= 0 {
		rpcFail(c, 400, "请求的租户标识未传递，请进行排查")
		return 0, false
	}
	if h.validateTenant != nil {
		if err := h.validateTenant(c.Request.Context(), id, time.Now()); err != nil {
			rpcFailErr(c, err)
			return 0, false
		}
	}
	return id, true
}

func (h *socialRPC) bind(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	var req struct {
		UserID     *int64 `json:"userId"`
		UserType   *int   `json:"userType"`
		SocialType *int   `json:"socialType"`
		Code       string `json:"code"`
		State      string `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rpcFail(c, 400, "请求参数不正确")
		return
	}
	if msg := requireBind(req.UserID, req.UserType, req.SocialType, req.Code, req.State); msg != "" {
		rpcFail(c, 400, msg)
		return
	}
	openID, err := h.svc.BindUser(c.Request.Context(), tenantID, *req.UserID, *req.UserType, *req.SocialType, req.Code, req.State)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, openID)
}

func (h *socialRPC) unbind(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	var req struct {
		UserID     *int64 `json:"userId"`
		UserType   *int   `json:"userType"`
		SocialType *int   `json:"socialType"`
		OpenID     string `json:"openid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rpcFail(c, 400, "请求参数不正确")
		return
	}
	if req.UserID == nil {
		rpcFail(c, 400, "用户编号不能为空")
		return
	}
	if msg := requireUserAndSocial(req.UserType, req.SocialType); msg != "" {
		rpcFail(c, 400, msg)
		return
	}
	if req.OpenID == "" {
		rpcFail(c, 400, "社交平台的 openid 不能为空")
		return
	}
	if err := h.svc.UnbindUser(c.Request.Context(), tenantID, *req.UserID, *req.UserType, *req.SocialType, req.OpenID); err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *socialRPC) byUser(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	userID, ok := rpcQueryInt64(c, "userId")
	if !ok {
		return
	}
	socialType, ok := rpcQueryInt(c, "socialType")
	if !ok {
		return
	}
	item, err := h.svc.SocialByUser(c.Request.Context(), tenantID, userID, userType, socialType)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, socialDTO(item))
}

func (h *socialRPC) byCode(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	socialType, ok := rpcQueryInt(c, "socialType")
	if !ok {
		return
	}
	code, ok := rpcQueryText(c, "code")
	if !ok {
		return
	}
	state, ok := rpcQueryText(c, "state")
	if !ok {
		return
	}
	item, err := h.svc.SocialByCode(c.Request.Context(), tenantID, userType, socialType, code, state)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, socialDTO(item))
}

func (h *socialRPC) authorizeURL(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	socialType, ok := rpcQueryInt(c, "socialType")
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	redirectURI, ok := rpcQueryText(c, "redirectUri")
	if !ok {
		return
	}
	link, err := h.svc.Redirect(c.Request.Context(), tenantID, socialType, userType, redirectURI)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, link)
}

func requireBind(userID *int64, userType, socialType *int, code, state string) string {
	if userID == nil {
		return "用户编号不能为空"
	}
	if msg := requireUserAndSocial(userType, socialType); msg != "" {
		return msg
	}
	if code == "" {
		return "授权码不能为空"
	}
	if state == "" {
		return "state 不能为空"
	}
	return ""
}

func requireUserAndSocial(userType, socialType *int) string {
	if userType == nil {
		return "用户类型不能为空"
	}
	if _, ok := rpcUserTypes[*userType]; !ok {
		return userTypeRangeMsg
	}
	if socialType == nil {
		return "社交平台的类型不能为空"
	}
	if _, ok := rpcSocialTypes[*socialType]; !ok {
		return socialTypeRangeMsg
	}
	return ""
}

func socialDTO(item *SocialView) *socialUserDTO {
	if item == nil {
		return nil
	}
	return &socialUserDTO{OpenID: item.OpenID, Nickname: item.Nickname, Avatar: item.Avatar, UserID: item.UserID}
}

func rpcQueryText(c *gin.Context, name string) (string, bool) {
	value, present := c.GetQuery(name)
	if !present {
		rpcFail(c, 400, "请求参数缺失:"+name)
		return "", false
	}
	return value, true
}

func rpcQueryInt(c *gin.Context, name string) (int, bool) {
	raw, ok := rpcQueryText(c, name)
	if !ok {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		rpcFail(c, 400, "请求参数类型错误:"+name)
		return 0, false
	}
	return value, true
}

func rpcQueryInt64(c *gin.Context, name string) (int64, bool) {
	raw, ok := rpcQueryText(c, name)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		rpcFail(c, 400, "请求参数类型错误:"+name)
		return 0, false
	}
	return value, true
}

func rpcFail(c *gin.Context, code int, msg string) {
	httpx.Fail(c, http.StatusOK, code, msg)
}

func rpcFailErr(c *gin.Context, err error) {
	switch item := err.(type) {
	case *Error:
		rpcFail(c, item.Code, item.Msg)
	case *directory.Error:
		rpcFail(c, item.Code, item.Msg)
	default:
		rpcFail(c, 500, "系统异常")
	}
}
