package datasource

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上数据源配置。主库编号 0 来自当前进程，不在表里。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	g := r.Group("/admin-api/infra/data-source-config")
	g.POST("/create", h.permit("infra:data-source-config:create", h.create))
	g.PUT("/update", h.permit("infra:data-source-config:update", h.update))
	g.DELETE("/delete", h.permit("infra:data-source-config:delete", h.delete))
	g.DELETE("/delete-list", h.permit("infra:data-source-config:delete", h.deleteList))
	g.GET("/get", h.permit("infra:data-source-config:query", h.get))
	g.GET("/list", h.permit("infra:data-source-config:query", h.list))
}

type handler struct {
	sessions *auth.Service
	svc      *Service
}

func (h *handler) permit(perm string, next func(*gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, tenantID, perms, err := h.sessions.Session(c.Request.Context(), bearer(c))
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
		next(c)
	}
}

func (h *handler) create(c *gin.Context) {
	in, ok := bindSave(c, false)
	if !ok {
		return
	}
	id, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, id)
}

func (h *handler) update(c *gin.Context) {
	in, ok := bindSave(c, true)
	if !ok {
		return
	}
	if err := h.svc.Update(c.Request.Context(), in); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) delete(c *gin.Context) {
	if _, present := c.GetQuery("id"); !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:id"})
		return
	}
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数类型错误:id"})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) deleteList(c *gin.Context) {
	values, present := c.Request.URL.Query()["ids"]
	if !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:ids"})
		return
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
				return
			}
			ids = append(ids, id)
		}
	}
	if len(ids) > 1000 {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.DeleteList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) get(c *gin.Context) {
	if _, present := c.GetQuery("id"); !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:id"})
		return
	}
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数类型错误:id"})
		return
	}
	item, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) list(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		writeBiz(c, err)
		return
	}
	if list == nil {
		list = []Item{}
	}
	httpx.OK(c, list)
}

func bindSave(c *gin.Context, needID bool) (SaveInput, bool) {
	var req struct {
		ID       *int64  `json:"id"`
		Name     *string `json:"name"`
		URL      *string `json:"url"`
		Username *string `json:"username"`
		Password *string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return SaveInput{}, false
	}
	if req.Name == nil {
		writeBiz(c, &Error{Code: 400, Msg: "数据源名称不能为空"})
		return SaveInput{}, false
	}
	if req.URL == nil {
		writeBiz(c, &Error{Code: 400, Msg: "数据源连接不能为空"})
		return SaveInput{}, false
	}
	if req.Username == nil {
		writeBiz(c, &Error{Code: 400, Msg: "用户名不能为空"})
		return SaveInput{}, false
	}
	if req.Password == nil {
		writeBiz(c, &Error{Code: 400, Msg: "密码不能为空"})
		return SaveInput{}, false
	}
	in := SaveInput{Name: *req.Name, URL: *req.URL, Username: *req.Username, Password: *req.Password}
	if needID {
		if req.ID == nil {
			writeBiz(c, &Error{Code: codeMiss, Msg: "数据源配置不存在"})
			return SaveInput{}, false
		}
		in.ID = *req.ID
	}
	return in, true
}

func writeBiz(c *gin.Context, err error) {
	if biz, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 500, "系统异常")
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
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return ""
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
