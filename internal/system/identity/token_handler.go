package identity

import (
	"encoding/base64"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func (h *handler) token(c *gin.Context) {
	clientID, secret, ok := basicClient(c)
	if !ok {
		writeBiz(c, &Error{Code: 400, Msg: "client_id 或 client_secret 未正确传递"})
		return
	}
	req := TokenRequest{
		GrantType:   formValue(c, "grant_type"),
		Code:        formValue(c, "code"),
		RedirectURI: formValue(c, "redirect_uri"),
		State:       formValue(c, "state"),
		Username:    formValue(c, "username"),
		Password:    formValue(c, "password"),
		Scope:       formValue(c, "scope"),
		Refresh:     formValue(c, "refresh_token"),
		ClientID:    clientID,
		Secret:      secret,
	}
	if id, ok := headerTenant(c); ok {
		req.TenantID = id
	}
	if req.GrantType == "" {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	token, err := h.svc.IssueToken(c.Request.Context(), req)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, token)
}

func (h *handler) checkToken(c *gin.Context) {
	clientID, secret, ok := basicClient(c)
	if !ok {
		writeBiz(c, &Error{Code: 400, Msg: "client_id 或 client_secret 未正确传递"})
		return
	}
	access := formValue(c, "token")
	if access == "" {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:token"})
		return
	}
	item, err := h.svc.CheckOpenToken(c.Request.Context(), clientID, secret, access)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) revokeToken(c *gin.Context) {
	clientID, secret, ok := basicClient(c)
	if !ok {
		writeBiz(c, &Error{Code: 400, Msg: "client_id 或 client_secret 未正确传递"})
		return
	}
	access := formValue(c, "token")
	if access == "" {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:token"})
		return
	}
	removed, err := h.svc.RevokeOpenToken(c.Request.Context(), clientID, secret, access)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, removed)
}

func basicClient(c *gin.Context) (string, string, bool) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Basic ") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, "Basic "))
	if err != nil {
		return "", "", false
	}
	clientID, secret, ok := strings.Cut(string(raw), ":")
	if !ok || clientID == "" {
		return "", "", false
	}
	return clientID, secret, true
}

func formValue(c *gin.Context, key string) string {
	if value := c.PostForm(key); value != "" {
		return value
	}
	return c.Query(key)
}
