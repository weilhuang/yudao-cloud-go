package demo02

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上示例分类。列表和导出使用同一套名字、父级和创建时间筛选。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	g := r.Group("/admin-api/infra/demo02-category")
	g.POST("/create", h.permit("infra:demo02-category:create", h.create))
	g.PUT("/update", h.permit("infra:demo02-category:update", h.update))
	g.DELETE("/delete", h.permit("infra:demo02-category:delete", h.delete))
	g.GET("/get", h.permit("infra:demo02-category:query", h.get))
	g.GET("/list", h.permit("infra:demo02-category:query", h.list))
	g.GET("/export-excel", h.permit("infra:demo02-category:export", h.export))
}

type handler struct {
	sessions *auth.Service
	svc      *Service
}

func (h *handler) permit(perm string, next func(*gin.Context, int64)) gin.HandlerFunc {
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
		next(c, tenantID)
	}
}

func (h *handler) create(c *gin.Context, tenantID int64) {
	in, ok := bindSave(c, false)
	if !ok {
		return
	}
	id, err := h.svc.Create(c.Request.Context(), tenantID, in)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, id)
}

func (h *handler) update(c *gin.Context, tenantID int64) {
	in, ok := bindSave(c, true)
	if !ok {
		return
	}
	if err := h.svc.Update(c.Request.Context(), tenantID, in); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) delete(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), tenantID, id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) get(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "id")
	if !ok {
		return
	}
	item, err := h.svc.Get(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if item == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, categoryJSON(*item))
}

func (h *handler) list(c *gin.Context, tenantID int64) {
	list, err := h.svc.List(c.Request.Context(), tenantID, queryOf(c))
	if err != nil {
		writeBiz(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, categoryJSON(item))
	}
	httpx.OK(c, out)
}

func (h *handler) export(c *gin.Context, tenantID int64) {
	list, err := h.svc.List(c.Request.Context(), tenantID, queryOf(c))
	if err != nil {
		writeBiz(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "示例分类.xls", "数据", []string{"编号", "名字", "父级编号", "创建时间"},
		func(emit func([]sheet.XLSXCell) error) error {
			for _, item := range list {
				if err := emit([]sheet.XLSXCell{
					{Value: strconv.FormatInt(item.ID, 10)},
					{Value: item.Name},
					{Value: strconv.FormatInt(item.ParentID, 10)},
					{Value: excelTime(item.CreateTime)},
				}); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		writeBiz(c, err)
	}
}

func queryOf(c *gin.Context) Query {
	q := Query{Name: c.Query("name"), CreateFrom: timeBound(c, 0), CreateTo: timeBound(c, 1)}
	if text, present := c.GetQuery("parentId"); present && text != "" {
		if id, err := strconv.ParseInt(text, 10, 64); err == nil {
			q.ParentID = &id
		}
	}
	return q
}

func bindSave(c *gin.Context, update bool) (Save, bool) {
	var req struct {
		ID       *int64  `json:"id"`
		Name     *string `json:"name"`
		ParentID *int64  `json:"parentId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, biz(400, "请求参数不正确"))
		return Save{}, false
	}
	if req.Name == nil || *req.Name == "" {
		writeBiz(c, biz(400, "名字不能为空"))
		return Save{}, false
	}
	if req.ParentID == nil {
		writeBiz(c, biz(400, "父级编号不能为空"))
		return Save{}, false
	}
	in := Save{Name: *req.Name, ParentID: *req.ParentID}
	if update {
		if req.ID == nil || *req.ID <= 0 {
			writeBiz(c, biz(codeNotExists, "示例分类不存在"))
			return Save{}, false
		}
		in.ID = *req.ID
	}
	return in, true
}

func categoryJSON(item Category) gin.H {
	return gin.H{"id": item.ID, "name": item.Name, "parentId": item.ParentID, "createTime": item.CreateTime}
}

func excelTime(millis int64) string {
	if millis <= 0 {
		return ""
	}
	return time.UnixMilli(millis).In(shanghai()).Format("2006-01-02 15:04:05")
}

func queryID(c *gin.Context, name string) (int64, bool) {
	if _, present := c.GetQuery(name); !present {
		writeBiz(c, biz(400, "请求参数缺失:"+name))
		return 0, false
	}
	id, err := strconv.ParseInt(c.Query(name), 10, 64)
	if err != nil {
		writeBiz(c, biz(400, "请求参数类型错误:"+name))
		return 0, false
	}
	return id, true
}

func timeBound(c *gin.Context, index int) string {
	values := c.QueryArray("createTime")
	if len(values) == 0 {
		values = c.QueryArray("createTime[]")
	}
	if index >= len(values) {
		return ""
	}
	text := strings.TrimSpace(values[index])
	if _, err := time.ParseInLocation("2006-01-02 15:04:05", text, shanghai()); err != nil {
		return ""
	}
	return text
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
	return id, err == nil
}

func writeBiz(c *gin.Context, err error) {
	if item, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, item.Code, item.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 500, "系统异常")
}

func writeAuth(c *gin.Context, err error) {
	if item, ok := err.(*auth.Error); ok {
		httpx.Fail(c, http.StatusOK, item.Code, item.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 401, "账号未登录")
}
