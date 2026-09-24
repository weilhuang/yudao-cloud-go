package config

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上参数配置。按键取值不校验具体权限，但不可见配置仍不返回。
func Mount(r *gin.Engine, sessions *auth.Service, store Store) {
	h := &handler{sessions: sessions, store: store}
	g := r.Group("/admin-api/infra/config")
	g.POST("/create", h.permit("infra:config:create", h.save))
	g.PUT("/update", h.permit("infra:config:update", h.save))
	g.DELETE("/delete", h.permit("infra:config:delete", h.delete))
	g.DELETE("/delete-list", h.permit("infra:config:delete", h.deleteList))
	g.GET("/get", h.permit("infra:config:query", h.get))
	g.GET("/get-value-by-key", h.login(h.valueByKey))
	g.GET("/page", h.permit("infra:config:query", h.page))
	g.GET("/export-excel", h.permit("infra:config:export", h.export))

	r.GET("/rpc-api/infra/config/get-value-by-key", h.rpcValue)
}

type handler struct {
	sessions *auth.Service
	store    Store
}

type caller struct {
	tenantID int64
}

func (h *handler) login(next func(*gin.Context, caller)) gin.HandlerFunc {
	return h.permit("", next)
}

func (h *handler) permit(perm string, next func(*gin.Context, caller)) gin.HandlerFunc {
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
		next(c, caller{tenantID: tenantID})
	}
}

func (h *handler) save(c *gin.Context, _ caller) {
	var item Item
	if err := c.ShouldBindJSON(&item); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := Save(c.Request.Context(), h.store, item)
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

func (h *handler) delete(c *gin.Context, _ caller) {
	if err := Remove(c.Request.Context(), h.store, queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) deleteList(c *gin.Context, _ caller) {
	for _, id := range queryIDs(c) {
		if err := Remove(c.Request.Context(), h.store, id); err != nil {
			writeBiz(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

func (h *handler) get(c *gin.Context, _ caller) {
	item, err := h.store.ByID(c.Request.Context(), queryInt(c, "id"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) valueByKey(c *gin.Context, _ caller) {
	value, ok, err := VisibleValue(c.Request.Context(), h.store, c.Query("key"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	if !ok {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, value)
}

func (h *handler) rpcValue(c *gin.Context) {
	item, err := h.store.ByKey(c.Request.Context(), c.Query("key"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	if item == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, item.Value)
}

func (h *handler) page(c *gin.Context, _ caller) {
	page, err := h.store.Page(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), c.Query("key"), queryType(c), timeStart(c), timeEnd(c))
	writePage(c, page, err)
}

func (h *handler) export(c *gin.Context, _ caller) {
	page, err := h.store.Page(c.Request.Context(), 1, -1, c.Query("name"), c.Query("key"), queryType(c), timeStart(c), timeEnd(c))
	if err != nil {
		writeBiz(c, err)
		return
	}
	rows := make([][]string, 0, len(page.List))
	for _, item := range page.List {
		visible := "否"
		if item.Visible {
			visible = "是"
		}
		rows = append(rows, []string{sheet.Cell(item.ID), item.Category, item.Name, item.Key, item.Value, sheet.Cell(int64(item.Type)), visible})
	}
	sheet.Write(c, "参数配置.xls", []string{"编号", "分类", "名称", "键名", "键值", "类型", "是否可见"}, rows)
}

func queryType(c *gin.Context) *int {
	text := c.Query("type")
	if text == "" {
		return nil
	}
	value := atoi(text, 0)
	return &value
}

func timeStart(c *gin.Context) *int64 { return timeBound(c, 0) }
func timeEnd(c *gin.Context) *int64   { return timeBound(c, 1) }

func timeBound(c *gin.Context, index int) *int64 {
	values := c.QueryArray("createTime")
	if len(values) == 0 {
		if text := c.Query("createTime[0]"); text != "" {
			values = append(values, text)
		}
		if text := c.Query("createTime[1]"); text != "" {
			values = append(values, text)
		}
	}
	if len(values) <= index || values[index] == "" {
		return nil
	}
	n := int64(atoi(values[index], 0))
	return &n
}

func writePage(c *gin.Context, page Page[Item], err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if page.List == nil {
		page.List = []Item{}
	}
	httpx.OK(c, page)
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

func queryIDs(c *gin.Context) []int64 {
	var ids []int64
	for _, text := range c.QueryArray("ids") {
		for _, part := range strings.Split(text, ",") {
			if part == "" {
				continue
			}
			ids = append(ids, int64(atoi(part, 0)))
		}
	}
	return ids
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
