package apilog

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上访问日志、错误日志，以及给其他服务写入的 RPC。
func Mount(r *gin.Engine, sessions *auth.Service, store Store) {
	h := &handler{sessions: sessions, store: store}
	access := r.Group("/admin-api/infra/api-access-log")
	access.GET("/page", h.permit("infra:api-access-log:query", h.accessPage))
	access.GET("/get", h.permit("infra:api-access-log:query", h.accessGet))
	access.GET("/export-excel", h.permit("infra:api-access-log:export", h.accessExport))

	errlog := r.Group("/admin-api/infra/api-error-log")
	errlog.PUT("/update-status", h.permit("infra:api-error-log:update-status", h.errorStatus))
	errlog.GET("/page", h.permit("infra:api-error-log:query", h.errorPage))
	errlog.GET("/get", h.permit("infra:api-error-log:query", h.errorGet))
	errlog.GET("/export-excel", h.permit("infra:api-error-log:export", h.errorExport))

	rpc := r.Group("/rpc-api/infra")
	rpc.POST("/api-access-log/create", h.createAccess)
	rpc.POST("/api-error-log/create", h.createError)
}

type handler struct {
	sessions *auth.Service
	store    Store
}

type caller struct {
	userID   int64
	tenantID int64
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

func (h *handler) accessPage(c *gin.Context, who caller) {
	page, err := h.store.AccessPage(c.Request.Context(), who.tenantID, accessQuery(c))
	writePage(c, page, err)
}

func (h *handler) accessGet(c *gin.Context, who caller) {
	item, err := h.store.AccessByID(c.Request.Context(), who.tenantID, queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) accessExport(c *gin.Context, who caller) {
	q := accessQuery(c)
	q.PageSize = -1
	page, err := h.store.AccessPage(c.Request.Context(), who.tenantID, q)
	if err != nil {
		writeAuth(c, err)
		return
	}
	rows := make([][]string, 0, len(page.List))
	for _, item := range page.List {
		rows = append(rows, []string{sheet.Cell(item.ID), item.ApplicationName, item.RequestMethod, item.RequestURL, sheet.Cell(int64(item.Duration)), sheet.Cell(int64(item.ResultCode))})
	}
	sheet.Write(c, "API 访问日志.xls", []string{"编号", "应用名", "请求方法", "请求地址", "执行时长", "结果码"}, rows)
}

func (h *handler) errorStatus(c *gin.Context, who caller) {
	if err := MarkError(c.Request.Context(), h.store, who.tenantID, queryInt(c, "id"), who.userID, int(queryInt(c, "processStatus"))); err != nil {
		if biz, ok := err.(*Error); ok {
			httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
			return
		}
		writeAuth(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) errorPage(c *gin.Context, who caller) {
	page, err := h.store.ErrorPage(c.Request.Context(), who.tenantID, errorQuery(c))
	writePage(c, page, err)
}

func (h *handler) errorGet(c *gin.Context, who caller) {
	item, err := h.store.ErrorByID(c.Request.Context(), who.tenantID, queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) errorExport(c *gin.Context, who caller) {
	q := errorQuery(c)
	q.PageSize = -1
	page, err := h.store.ErrorPage(c.Request.Context(), who.tenantID, q)
	if err != nil {
		writeAuth(c, err)
		return
	}
	rows := make([][]string, 0, len(page.List))
	for _, item := range page.List {
		rows = append(rows, []string{sheet.Cell(item.ID), item.ApplicationName, item.RequestURL, sheet.Text(item.ExceptionMessage), sheet.Cell(int64(item.ProcessStatus))})
	}
	sheet.Write(c, "API 错误日志.xls", []string{"编号", "应用名", "请求地址", "异常信息", "处理状态"}, rows)
}

func (h *handler) createAccess(c *gin.Context) {
	var item AccessLog
	if err := c.ShouldBindJSON(&item); err != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return
	}
	tenantID, _ := headerTenant(c)
	if err := h.store.CreateAccess(c.Request.Context(), tenantID, item); err != nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	httpx.OK(c, true)
}

func (h *handler) createError(c *gin.Context) {
	var item ErrorLog
	if err := c.ShouldBindJSON(&item); err != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return
	}
	tenantID, _ := headerTenant(c)
	if err := h.store.CreateError(c.Request.Context(), tenantID, item); err != nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	httpx.OK(c, true)
}

func accessQuery(c *gin.Context) AccessQuery {
	q := AccessQuery{
		PageNo: atoi(c.Query("pageNo"), 1), PageSize: atoi(c.Query("pageSize"), 10),
		UserID: queryInt(c, "userId"), ApplicationName: c.Query("applicationName"), RequestURL: c.Query("requestUrl"),
	}
	q.Start, q.End = timeRange(c, "beginTime")
	if text := c.Query("userType"); text != "" {
		value := atoi(text, 0)
		q.UserType = &value
	}
	if text := c.Query("duration"); text != "" {
		value := atoi(text, 0)
		q.Duration = &value
	}
	if text := c.Query("resultCode"); text != "" {
		value := atoi(text, 0)
		q.ResultCode = &value
	}
	return q
}

func errorQuery(c *gin.Context) ErrorQuery {
	q := ErrorQuery{
		PageNo: atoi(c.Query("pageNo"), 1), PageSize: atoi(c.Query("pageSize"), 10),
		UserID: queryInt(c, "userId"), ApplicationName: c.Query("applicationName"), RequestURL: c.Query("requestUrl"),
	}
	q.Start, q.End = timeRange(c, "exceptionTime")
	if text := c.Query("userType"); text != "" {
		value := atoi(text, 0)
		q.UserType = &value
	}
	if text := c.Query("processStatus"); text != "" {
		value := atoi(text, 0)
		q.ProcessStatus = &value
	}
	return q
}

func timeRange(c *gin.Context, name string) (*int64, *int64) {
	values := c.QueryArray(name)
	if len(values) == 0 {
		if text := c.Query(name + "[0]"); text != "" {
			values = append(values, text)
		}
		if text := c.Query(name + "[1]"); text != "" {
			values = append(values, text)
		}
	}
	var start, end *int64
	if len(values) > 0 && values[0] != "" {
		n := int64(atoi(values[0], 0))
		start = &n
	}
	if len(values) > 1 && values[1] != "" {
		n := int64(atoi(values[1], 0))
		end = &n
	}
	return start, end
}

func writePage[T any](c *gin.Context, page Page[T], err error) {
	if err != nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	if page.List == nil {
		page.List = []T{}
	}
	httpx.OK(c, page)
}

func writeOne[T any](c *gin.Context, item *T, err error) {
	if err != nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	httpx.OK(c, item)
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
