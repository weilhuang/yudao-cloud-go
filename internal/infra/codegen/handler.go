package codegen

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上代码生成管理接口。预览和下载目前只包含 SQL 文件。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	g := r.Group("/admin-api/infra/codegen")
	g.GET("/db/table/list", h.permit("infra:codegen:query", h.dbList))
	g.GET("/table/list", h.permit("infra:codegen:query", h.tableList))
	g.GET("/table/page", h.permit("infra:codegen:query", h.tablePage))
	g.GET("/detail", h.permit("infra:codegen:query", h.detail))
	g.POST("/create-list", h.permit("infra:codegen:create", h.createList))
	g.PUT("/update", h.permit("infra:codegen:update", h.update))
	g.PUT("/sync-from-db", h.permit("infra:codegen:update", h.sync))
	g.DELETE("/delete", h.permit("infra:codegen:delete", h.delete))
	g.DELETE("/delete-list", h.permit("infra:codegen:delete", h.deleteList))
	g.GET("/preview", h.permit("infra:codegen:preview", h.preview))
	g.GET("/download", h.permit("infra:codegen:download", h.download))
}

type handler struct {
	sessions *auth.Service
	svc      *Service
}

func (h *handler) permit(perm string, next func(*gin.Context, int64)) gin.HandlerFunc {
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
		next(c, userID)
	}
}

func (h *handler) dbList(c *gin.Context, _ int64) {
	if _, present := c.GetQuery("dataSourceConfigId"); !present {
		writeBiz(c, biz(400, "请求参数缺失:dataSourceConfigId"))
		return
	}
	id, err := strconv.ParseInt(c.Query("dataSourceConfigId"), 10, 64)
	if err != nil {
		writeBiz(c, biz(400, "请求参数类型错误:dataSourceConfigId"))
		return
	}
	list, err := h.svc.DatabaseTables(c.Request.Context(), id, c.Query("name"), c.Query("comment"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, gin.H{"name": item.Name, "comment": item.Comment})
	}
	httpx.OK(c, out)
}

func (h *handler) tableList(c *gin.Context, _ int64) {
	if _, present := c.GetQuery("dataSourceConfigId"); !present {
		writeBiz(c, biz(400, "请求参数缺失:dataSourceConfigId"))
		return
	}
	id, err := strconv.ParseInt(c.Query("dataSourceConfigId"), 10, 64)
	if err != nil {
		writeBiz(c, biz(400, "请求参数类型错误:dataSourceConfigId"))
		return
	}
	list, err := h.svc.List(c.Request.Context(), id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, tablesJSON(list))
}

func (h *handler) tablePage(c *gin.Context, _ int64) {
	page, err := h.svc.Page(c.Request.Context(), PageQuery{
		PageNo:       atoi(c.Query("pageNo"), 1),
		PageSize:     atoi(c.Query("pageSize"), 10),
		TableName:    c.Query("tableName"),
		TableComment: c.Query("tableComment"),
		ClassName:    c.Query("className"),
		CreateFrom:   createBound(c, 0),
		CreateTo:     createBound(c, 1),
	})
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, gin.H{"list": tablesJSON(page.List), "total": page.Total})
}

func (h *handler) detail(c *gin.Context, _ int64) {
	id, ok := queryID(c, "tableId")
	if !ok {
		return
	}
	table, columns, err := h.svc.Detail(c.Request.Context(), id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if table == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, gin.H{"table": tableJSON(*table), "columns": columnsJSON(columns)})
}

func (h *handler) createList(c *gin.Context, userID int64) {
	var req struct {
		DataSourceConfigID *int64   `json:"dataSourceConfigId"`
		TableNames         []string `json:"tableNames"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, biz(400, "请求参数不正确"))
		return
	}
	if req.DataSourceConfigID == nil {
		writeBiz(c, biz(400, "数据源配置的编号不能为空"))
		return
	}
	if req.TableNames == nil {
		writeBiz(c, biz(400, "表名数组不能为空"))
		return
	}
	ids, err := h.svc.CreateList(c.Request.Context(), userID, *req.DataSourceConfigID, req.TableNames)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, ids)
}

func (h *handler) update(c *gin.Context, _ int64) {
	var req struct {
		Table   *tableBody   `json:"table"`
		Columns []columnBody `json:"columns"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, biz(400, "请求参数不正确"))
		return
	}
	if req.Table == nil {
		writeBiz(c, biz(400, "表定义不能为空"))
		return
	}
	if req.Columns == nil {
		writeBiz(c, biz(400, "字段定义不能为空"))
		return
	}
	if err := req.Table.validate(); err != nil {
		writeBiz(c, err)
		return
	}
	columns := make([]Column, 0, len(req.Columns))
	for _, column := range req.Columns {
		item, err := column.toColumn()
		if err != nil {
			writeBiz(c, err)
			return
		}
		columns = append(columns, item)
	}
	if err := h.svc.Update(c.Request.Context(), req.Table.toTable(), columns); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) sync(c *gin.Context, _ int64) {
	id, ok := queryID(c, "tableId")
	if !ok {
		return
	}
	if err := h.svc.Sync(c.Request.Context(), id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) delete(c *gin.Context, _ int64) {
	id, ok := queryID(c, "tableId")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) deleteList(c *gin.Context, _ int64) {
	values, present := c.Request.URL.Query()["tableIds"]
	if !present {
		writeBiz(c, biz(400, "请求参数缺失:tableIds"))
		return
	}
	var ids []int64
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				writeBiz(c, biz(400, "请求参数不正确"))
				return
			}
			ids = append(ids, id)
		}
	}
	if err := h.svc.DeleteList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) preview(c *gin.Context, _ int64) {
	id, ok := queryID(c, "tableId")
	if !ok {
		return
	}
	files, err := h.svc.Preview(c.Request.Context(), id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	out := make([]gin.H, 0, len(files))
	for _, file := range files {
		out = append(out, gin.H{"filePath": file.FilePath, "code": file.Code})
	}
	httpx.OK(c, out)
}

func (h *handler) download(c *gin.Context, _ int64) {
	id, ok := queryID(c, "tableId")
	if !ok {
		return
	}
	body, err := h.svc.Download(c.Request.Context(), id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", "attachment;filename=\"codegen.zip\";filename*=UTF-8''codegen.zip")
	c.Data(http.StatusOK, "application/zip", body)
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

func atoi(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func createBound(c *gin.Context, index int) string {
	values := c.QueryArray("createTime")
	if len(values) == 0 {
		values = c.QueryArray("createTime[]")
	}
	if index >= len(values) {
		return ""
	}
	text := strings.TrimSpace(values[index])
	if text == "" {
		return ""
	}
	if _, err := time.ParseInLocation("2006-01-02 15:04:05", text, shanghai()); err != nil {
		return ""
	}
	return text
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
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

func writeBiz(c *gin.Context, err error) {
	if bizErr, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, bizErr.Code, bizErr.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 500, "系统异常")
}

func writeAuth(c *gin.Context, err error) {
	if biz, ok := err.(*auth.Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 401, "账号未登录")
}
