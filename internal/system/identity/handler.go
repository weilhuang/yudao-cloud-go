package identity

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上 OAuth2 客户端、社交客户端，以及登录页用的社交跳转。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service, db *MySQL) {
	h := &handler{sessions: sessions, svc: svc, db: db}
	client := r.Group("/admin-api/system/oauth2-client")
	client.POST("/create", h.permit("system:oauth2-client:create", h.clientSave))
	client.PUT("/update", h.permit("system:oauth2-client:update", h.clientSave))
	client.DELETE("/delete", h.permit("system:oauth2-client:delete", h.clientDelete))
	client.DELETE("/delete-list", h.permit("system:oauth2-client:delete", h.clientDeleteList))
	client.GET("/get", h.permit("system:oauth2-client:query", h.clientGet))
	client.GET("/page", h.permit("system:oauth2-client:query", h.clientPage))

	social := r.Group("/admin-api/system/social-client")
	social.POST("/create", h.permit("system:social-client:create", h.socialSave))
	social.PUT("/update", h.permit("system:social-client:update", h.socialSave))
	social.DELETE("/delete", h.permit("system:social-client:delete", h.socialDelete))
	social.DELETE("/delete-list", h.permit("system:social-client:delete", h.socialDeleteList))
	social.GET("/get", h.permit("system:social-client:query", h.socialGet))
	social.GET("/page", h.permit("system:social-client:query", h.socialPage))
	social.POST("/send-subscribe-message", h.permit("system:social-client:query", h.sendSubscribe))

	user := r.Group("/admin-api/system/social-user")
	user.POST("/bind", h.login(h.bind))
	user.GET("/get-bind-list", h.login(h.bindList))
	user.DELETE("/unbind", h.login(h.unbind))
	user.GET("/get", h.permit("system:social-user:query", h.socialUserGet))
	user.GET("/page", h.permit("system:social-user:query", h.socialUserPage))

	authn := r.Group("/admin-api/system/auth")
	authn.GET("/social-auth-redirect", h.redirect)
	authn.POST("/social-login", h.socialLogin)

	open := r.Group("/admin-api/system/oauth2")
	open.GET("/authorize", h.login(h.authorizeInfo))
	open.POST("/authorize", h.login(h.authorize))
	open.POST("/token", h.token)
	open.DELETE("/token", h.revokeToken)
	open.POST("/check-token", h.checkToken)
}

type handler struct {
	sessions *auth.Service
	svc      *Service
	db       *MySQL
}

type caller struct {
	userID   int64
	tenantID int64
}

func (h *handler) login(next func(*gin.Context, caller)) gin.HandlerFunc {
	return h.permit("", next)
}

func (h *handler) permit(perm string, next func(*gin.Context, caller)) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, tenantID, perms, err := h.sessions.Session(c.Request.Context(), bearer(c))
		if err != nil {
			writeAuth(c, err)
			return
		}
		header, ok := headerTenant(c)
		if !ok || header != tenantID {
			msg := "请求的租户标识未传递，请进行排查"
			code := 400
			if ok {
				msg = "您无权访问该租户的数据"
				code = 403
			}
			httpx.Fail(c, http.StatusOK, code, msg)
			return
		}
		if perm != "" && !perms[perm] {
			httpx.Fail(c, http.StatusOK, 403, "没有该操作权限")
			return
		}
		next(c, caller{userID: userID, tenantID: tenantID})
	}
}

func (h *handler) clientSave(c *gin.Context, _ caller) {
	var item OAuthClient
	if err := c.ShouldBindJSON(&item); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveClient(c.Request.Context(), item)
	writeID(c, id, err)
}

func (h *handler) clientDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteClient(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) clientGet(c *gin.Context, _ caller) {
	item, err := h.db.ClientByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) clientPage(c *gin.Context, _ caller) {
	page, err := h.db.ClientPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) socialSave(c *gin.Context, who caller) {
	var item SocialClient
	if err := c.ShouldBindJSON(&item); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveSocial(c.Request.Context(), who.tenantID, item)
	writeID(c, id, err)
}

func (h *handler) socialDelete(c *gin.Context, who caller) {
	if err := h.db.DeleteSocial(c.Request.Context(), who.tenantID, queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) socialGet(c *gin.Context, who caller) {
	item, err := h.db.SocialByID(c.Request.Context(), who.tenantID, queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) socialPage(c *gin.Context, who caller) {
	var socialType *int
	if text := c.Query("socialType"); text != "" {
		value := atoi(text, 0)
		socialType = &value
	}
	page, err := h.db.SocialPage(c.Request.Context(), who.tenantID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), socialType)
	writePage(c, page, err)
}

func (h *handler) bindList(c *gin.Context, who caller) {
	list, err := h.db.BindList(c.Request.Context(), who.tenantID, who.userID)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if list == nil {
		list = []SocialUser{}
	}
	httpx.OK(c, list)
}

func (h *handler) bind(c *gin.Context, who caller) {
	var req struct {
		Type  *int   `json:"type"`
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Type == nil {
		msg := "请求参数不正确"
		if err == nil {
			msg = "社交平台的类型不能为空"
		}
		writeBiz(c, &Error{Code: 400, Msg: msg})
		return
	}
	if err := h.svc.BindCurrentUser(c.Request.Context(), who.tenantID, who.userID, *req.Type, req.Code, req.State); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) unbind(c *gin.Context, who caller) {
	var req struct {
		Type   *int   `json:"type"`
		OpenID string `json:"openid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Type == nil {
		msg := "请求参数不正确"
		if err == nil {
			msg = "社交平台的类型不能为空"
		}
		writeBiz(c, &Error{Code: 400, Msg: msg})
		return
	}
	if req.OpenID == "" {
		writeBiz(c, &Error{Code: 400, Msg: "社交用户的 openid 不能为空"})
		return
	}
	if err := h.svc.UnbindCurrentUser(c.Request.Context(), who.tenantID, who.userID, *req.Type, req.OpenID); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) socialUserGet(c *gin.Context, who caller) {
	if _, present := c.GetQuery("id"); !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:id"})
		return
	}
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数类型错误:id"})
		return
	}
	item, err := h.db.SocialUserByID(c.Request.Context(), who.tenantID, id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) socialUserPage(c *gin.Context, who caller) {
	page, err := h.db.SocialUserPage(c.Request.Context(), who.tenantID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("openid"))
	writePage(c, page, err)
}

func (h *handler) redirect(c *gin.Context) {
	tenantID, _ := headerTenant(c)
	link, err := h.svc.Redirect(c.Request.Context(), tenantID, atoi(c.Query("type"), 0), 2, c.Query("redirectUri"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, link)
}

func (h *handler) socialLogin(c *gin.Context) {
	var req struct {
		Type  int    `json:"type"`
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	tenantID, _ := headerTenant(c)
	userID, err := h.svc.LoginUser(c.Request.Context(), tenantID, req.Type, 2, req.Code, req.State)
	if err != nil {
		writeBiz(c, err)
		return
	}
	result, err := h.sessions.IssueFor(c.Request.Context(), userID)
	if err != nil {
		writeAuth(c, err)
		return
	}
	httpx.OK(c, gin.H{"userId": result.UserID, "accessToken": result.AccessToken, "refreshToken": result.RefreshToken, "expiresTime": result.ExpiresTime})
}

func writeID(c *gin.Context, id int64, err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if c.Request.Method == http.MethodPost {
		httpx.OK(c, id)
		return
	}
	httpx.OK(c, true)
}

func writePage[T any](c *gin.Context, page Page[T], err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if page.List == nil {
		page.List = []T{}
	}
	httpx.OK(c, page)
}

func writeOne[T any](c *gin.Context, item *T, err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func writeBiz(c *gin.Context, err error) {
	if biz, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	writeAuth(c, err)
}

func writeAuth(c *gin.Context, err error) {
	if biz, ok := err.(*auth.Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 500, "系统异常")
}

func bearer(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if len(header) > 7 && header[:7] == "Bearer " {
		return header[7:]
	}
	return ""
}

func headerTenant(c *gin.Context) (int64, bool) {
	text := c.GetHeader("tenant-id")
	if text == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	return id, err == nil
}

func queryInt(c *gin.Context, name string) int64 {
	n, _ := strconv.ParseInt(c.Query(name), 10, 64)
	return n
}

func queryStatus(c *gin.Context) *int {
	text := c.Query("status")
	if text == "" {
		return nil
	}
	value := atoi(text, 0)
	return &value
}

func atoi(text string, fallback int) int {
	if text == "" {
		return fallback
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return fallback
	}
	return n
}
