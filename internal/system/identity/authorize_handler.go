package identity

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func (h *handler) authorizeInfo(c *gin.Context, who caller) {
	if _, present := c.GetQuery("clientId"); !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:clientId"})
		return
	}
	info, err := h.svc.AuthorizeInfo(c.Request.Context(), who.tenantID, who.userID, c.Query("clientId"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, info)
}

func (h *handler) authorize(c *gin.Context, who caller) {
	responseType := c.Query("response_type")
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	if responseType == "" || clientID == "" || redirectURI == "" || c.Query("auto_approve") == "" {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	auto, err := strconv.ParseBool(c.Query("auto_approve"))
	if err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	scopes, err := parseScopes(c.Query("scope"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	link, err := h.svc.Approve(c.Request.Context(), who.tenantID, who.userID, responseType, clientID, redirectURI, c.Query("state"), auto, scopes)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if link == "" {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, link)
}
