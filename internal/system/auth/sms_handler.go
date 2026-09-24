package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func register(c *gin.Context, svc *Service) {
	var req struct {
		Username            string `json:"username"`
		Nickname            string `json:"nickname"`
		Password            string `json:"password"`
		CaptchaVerification string `json:"captchaVerification"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	result, err := svc.Register(c.Request.Context(), tenantID(c), req.Username, req.Nickname, req.Password, req.CaptchaVerification, requestMeta(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	writeLogin(c, result)
}

func smsLogin(c *gin.Context, svc *Service) {
	var req struct {
		Mobile string `json:"mobile"`
		Code   string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	result, err := svc.SmsLogin(c.Request.Context(), tenantID(c), req.Mobile, req.Code, c.ClientIP(), requestMeta(c))
	if err != nil {
		writeErr(c, err)
		return
	}
	writeLogin(c, result)
}

func sendSmsCode(c *gin.Context, svc *Service) {
	var req struct {
		Mobile              string `json:"mobile"`
		Scene               *int   `json:"scene"`
		CaptchaVerification string `json:"captchaVerification"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Scene == nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	if err := svc.SendSmsCode(c.Request.Context(), tenantID(c), req.Mobile, *req.Scene, req.CaptchaVerification, c.ClientIP()); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func resetPassword(c *gin.Context, svc *Service) {
	var req struct {
		Password string `json:"password"`
		Mobile   string `json:"mobile"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	if err := svc.ResetPassword(c.Request.Context(), tenantID(c), req.Mobile, req.Code, req.Password, c.ClientIP()); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func writeLogin(c *gin.Context, result *LoginResult) {
	httpx.OK(c, gin.H{
		"userId":       result.UserID,
		"accessToken":  result.AccessToken,
		"refreshToken": result.RefreshToken,
		"expiresTime":  result.ExpiresTime,
	})
}
