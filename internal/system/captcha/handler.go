package captcha

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Mount 挂上滑块验证码。响应是 AJ-Captcha 的 repCode，不包进 CommonResult。
func Mount(r *gin.Engine, svc *Service) {
	g := r.Group("/admin-api/system/captcha")
	g.POST("/get", func(c *gin.Context) {
		item, err := svc.Get(c.Request.Context())
		if err != nil {
			write(c, false, "6111", err.Error(), nil)
			return
		}
		write(c, true, "0000", "", item)
	})
	g.POST("/check", func(c *gin.Context) {
		var req struct {
			Token     string `json:"token"`
			PointJSON string `json:"pointJson"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			write(c, false, "6110", "请求参数不正确", nil)
			return
		}
		verification, err := svc.Check(c.Request.Context(), req.Token, req.PointJSON)
		if err != nil {
			write(c, false, "6111", err.Error(), nil)
			return
		}
		write(c, true, "0000", "", gin.H{"captchaVerification": verification, "token": req.Token, "result": true})
	})
}

func write(c *gin.Context, success bool, code, msg string, data any) {
	c.JSON(http.StatusOK, gin.H{"repCode": code, "repMsg": msg, "repData": data, "success": success})
}
