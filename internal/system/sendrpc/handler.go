package sendrpc

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
	"github.com/weilhuang/yudao-cloud-go/internal/system/message"
)

const (
	userMember = 1
	userAdmin  = 2
)

// ContactReader 只暴露发送接口查收件人所需的数据，HTTP 不关心数据源。
type ContactReader interface {
	AdminMobile(ctx context.Context, tenantID, userID int64) (string, error)
	AdminEmail(ctx context.Context, tenantID, userID int64) (string, error)
}

// Mount 挂上邮件、站内信、短信发送和短信验证码的 Feign 路由。
func Mount(r *gin.Engine, codes *auth.Service, sender *message.Service, contacts ContactReader, validateTenant func(context.Context, int64, time.Time) error) {
	h := handler{codes: codes, sender: sender, contacts: contacts, validateTenant: validateTenant}
	mail := r.Group("/rpc-api/system/mail/send")
	mail.POST("/send-single-admin", h.mail(userAdmin))
	mail.POST("/send-single-member", h.mail(userMember))
	notify := r.Group("/rpc-api/system/notify/send")
	notify.POST("/send-single-admin", h.notify(userAdmin))
	notify.POST("/send-single-member", h.notify(userMember))
	sms := r.Group("/rpc-api/system/sms/send")
	sms.POST("/send-single-admin", h.sms(userAdmin))
	sms.POST("/send-single-member", h.sms(userMember))
	code := r.Group("/rpc-api/system/oauth2/sms/code")
	code.POST("/send", h.codeSend)
	code.PUT("/use", h.codeUse)
	code.GET("/validate", h.codeValidate)
}

type handler struct {
	codes          *auth.Service
	sender         *message.Service
	contacts       ContactReader
	validateTenant func(context.Context, int64, time.Time) error
}

func (h handler) tenant(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.GetHeader("tenant-id"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, 400, "请求的租户标识未传递，请进行排查")
		return 0, false
	}
	if h.validateTenant != nil {
		if err := h.validateTenant(c.Request.Context(), id, time.Now()); err != nil {
			failErr(c, err)
			return 0, false
		}
	}
	return id, true
}

func (h handler) mail(userType int) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, ok := h.tenant(c)
		if !ok {
			return
		}
		var req struct {
			UserID         *int64         `json:"userId"`
			ToMails        []string       `json:"toMails"`
			CcMails        []string       `json:"ccMails"`
			BccMails       []string       `json:"bccMails"`
			TemplateCode   *string        `json:"templateCode"`
			TemplateParams map[string]any `json:"templateParams"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.TemplateCode == nil || *req.TemplateCode == "" {
			fail(c, 400, "邮件模板编号不能为空")
			return
		}
		to := append([]string{}, req.ToMails...)
		if userType == userAdmin && req.UserID != nil && *req.UserID > 0 && h.contacts != nil {
			email, err := h.contacts.AdminEmail(c.Request.Context(), tenantID, *req.UserID)
			if err != nil {
				failErr(c, err)
				return
			}
			if email != "" {
				to = append([]string{email}, to...)
			}
		}
		to = append(to, req.CcMails...)
		to = append(to, req.BccMails...)
		to = compact(to)
		id, err := h.sender.SendMail(c.Request.Context(), to, *req.TemplateCode, req.TemplateParams)
		if err != nil {
			failErr(c, err)
			return
		}
		httpx.OK(c, id)
	}
}

func (h handler) notify(userType int) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, ok := h.tenant(c)
		if !ok {
			return
		}
		var req struct {
			UserID         *int64         `json:"userId"`
			TemplateCode   *string        `json:"templateCode"`
			TemplateParams map[string]any `json:"templateParams"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, 400, "请求参数不正确")
			return
		}
		if req.UserID == nil {
			fail(c, 400, "用户编号不能为空")
			return
		}
		if req.TemplateCode == nil || *req.TemplateCode == "" {
			fail(c, 400, "站内信模板编号不能为空")
			return
		}
		id, err := h.sender.SendNotify(c.Request.Context(), tenantID, *req.UserID, userType, *req.TemplateCode, req.TemplateParams)
		if err != nil {
			failErr(c, err)
			return
		}
		if id == 0 {
			httpx.OK(c, nil)
			return
		}
		httpx.OK(c, id)
	}
}

func (h handler) sms(userType int) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, ok := h.tenant(c)
		if !ok {
			return
		}
		var req struct {
			UserID         *int64         `json:"userId"`
			Mobile         string         `json:"mobile"`
			TemplateCode   *string        `json:"templateCode"`
			TemplateParams map[string]any `json:"templateParams"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.TemplateCode == nil || *req.TemplateCode == "" {
			fail(c, 400, "短信模板编号不能为空")
			return
		}
		mobile := strings.TrimSpace(req.Mobile)
		userID := int64(0)
		if req.UserID != nil {
			userID = *req.UserID
		}
		if mobile == "" && userType == userAdmin && userID > 0 && h.contacts != nil {
			loaded, err := h.contacts.AdminMobile(c.Request.Context(), tenantID, userID)
			if err != nil {
				failErr(c, err)
				return
			}
			mobile = loaded
		}
		id, err := h.sender.SendSmsTo(c.Request.Context(), mobile, *req.TemplateCode, req.TemplateParams, userID, userType)
		if err != nil {
			failErr(c, err)
			return
		}
		httpx.OK(c, id)
	}
}

func (h handler) codeSend(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	var req struct {
		Mobile   string `json:"mobile"`
		Scene    *int   `json:"scene"`
		CreateIP string `json:"createIp"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Scene == nil {
		fail(c, 400, "发送场景不能为空")
		return
	}
	if err := h.codes.SendSmsCodeRPC(c.Request.Context(), tenantID, req.Mobile, *req.Scene, req.CreateIP); err != nil {
		failErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h handler) codeUse(c *gin.Context) {
	if _, ok := h.tenant(c); !ok {
		return
	}
	var req struct {
		Mobile string `json:"mobile"`
		Scene  *int   `json:"scene"`
		Code   string `json:"code"`
		UsedIP string `json:"usedIp"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Scene == nil {
		fail(c, 400, "发送场景不能为空")
		return
	}
	if err := h.codes.UseSmsCodeRPC(c.Request.Context(), req.Mobile, req.Code, *req.Scene, req.UsedIP); err != nil {
		failErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h handler) codeValidate(c *gin.Context) {
	if _, ok := h.tenant(c); !ok {
		return
	}
	var req struct {
		Mobile string `json:"mobile"`
		Scene  *int   `json:"scene"`
		Code   string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Scene == nil {
		fail(c, 400, "发送场景不能为空")
		return
	}
	if err := h.codes.ValidateSmsCodeRPC(c.Request.Context(), req.Mobile, req.Code, *req.Scene); err != nil {
		failErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func compact(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func fail(c *gin.Context, code int, msg string) {
	httpx.Fail(c, http.StatusOK, code, msg)
}

func failErr(c *gin.Context, err error) {
	switch item := err.(type) {
	case *auth.Error:
		fail(c, item.Code, item.Msg)
	case *message.Error:
		fail(c, item.Code, item.Msg)
	default:
		fail(c, 500, "系统异常")
	}
}
