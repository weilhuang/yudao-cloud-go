package auth

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func tokenPage(c *gin.Context, svc *Service) {
	who, ok := adminCaller(c, svc, "system:oauth2-token:page")
	if !ok {
		return
	}
	query := AccessTokenQuery{PageNo: atoi(c.Query("pageNo"), 1), PageSize: atoi(c.Query("pageSize"), 10)}
	if text, present := c.GetQuery("userId"); present {
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			writeErr(c, badRequest("请求参数类型错误:userId"))
			return
		}
		query.UserID = &id
	}
	if text, present := c.GetQuery("userType"); present {
		value, err := strconv.Atoi(text)
		if err != nil {
			writeErr(c, badRequest("请求参数类型错误:userType"))
			return
		}
		query.UserType = &value
	}
	query.ClientID = c.Query("clientId")
	page, err := svc.AccessTokenPage(c.Request.Context(), who.tenantID, query)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, page)
}

func tokenDelete(c *gin.Context, svc *Service) {
	who, ok := adminCaller(c, svc, "system:oauth2-token:delete")
	if !ok {
		return
	}
	if _, present := c.GetQuery("accessToken"); !present {
		writeErr(c, badRequest("请求参数缺失:accessToken"))
		return
	}
	if err := svc.ForceLogout(c.Request.Context(), who.tenantID, c.Query("accessToken"), requestMeta(c)); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func tokenDeleteList(c *gin.Context, svc *Service) {
	who, ok := adminCaller(c, svc, "system:oauth2-token:delete")
	if !ok {
		return
	}
	values, present := c.Request.URL.Query()["accessTokens"]
	if !present {
		writeErr(c, badRequest("请求参数缺失:accessTokens"))
		return
	}
	if len(values) > 1000 {
		writeErr(c, badRequest("请求参数不正确"))
		return
	}
	for _, accessToken := range values {
		if err := svc.ForceLogout(c.Request.Context(), who.tenantID, accessToken, requestMeta(c)); err != nil {
			writeErr(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

type caller struct {
	userID   int64
	tenantID int64
}

func adminCaller(c *gin.Context, svc *Service, perm string) (caller, bool) {
	userID, tenantID, perms, err := svc.Session(c.Request.Context(), bearer(c))
	if err != nil {
		writeErr(c, err)
		return caller{}, false
	}
	header, ok := headerTenant(c)
	if !ok {
		writeErr(c, badRequest("请求的租户标识未传递，请进行排查"))
		return caller{}, false
	}
	if header != tenantID {
		writeErr(c, forbidden("您无权访问该租户的数据"))
		return caller{}, false
	}
	if perm != "" && !perms[perm] {
		writeErr(c, forbidden("没有该操作权限"))
		return caller{}, false
	}
	return caller{userID: userID, tenantID: tenantID}, true
}

func atoi(text string, fallback int) int {
	if text == "" {
		return fallback
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return fallback
	}
	return value
}
