package logger

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Store 是管理端查询和 RPC 写入用的存储。
type Store interface {
	RecordLogin(ctx context.Context, item auth.LoginRecord) error
	LoginPage(ctx context.Context, tenantID int64, q LoginQuery) (Page[LoginLog], error)
	LoginByID(ctx context.Context, tenantID, id int64) (*LoginLog, error)
	CreateOperate(ctx context.Context, tenantID int64, item OperateLog) error
	OperatePage(ctx context.Context, tenantID int64, q OperateQuery) (Page[OperateLog], error)
	OperateByID(ctx context.Context, tenantID, id int64) (*OperateLog, error)
}

// Mount 挂上登录日志、操作日志，以及给其他服务调用的创建接口。
func Mount(r *gin.Engine, sessions *auth.Service, store Store) {
	h := &handler{sessions: sessions, store: store}
	login := r.Group("/admin-api/system/login-log")
	login.GET("/page", h.permit("system:login-log:query", h.loginPage))
	login.GET("/get", h.permit("system:login-log:query", h.loginGet))
	login.GET("/export-excel", h.permit("system:login-log:export", h.loginExport))

	operate := r.Group("/admin-api/system/operate-log")
	operate.GET("/page", h.permit("system:operate-log:query", h.operatePage))
	operate.GET("/get", h.permit("system:operate-log:query", h.operateGet))
	operate.GET("/export-excel", h.permit("system:operate-log:export", h.operateExport))

	rpc := r.Group("/rpc-api/system")
	rpc.POST("/login-log/create", h.createLogin)
	rpc.POST("/operate-log/create", h.createOperate)
	rpc.GET("/operate-log/page", h.operateRPCPage)
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

func (h *handler) loginPage(c *gin.Context, who caller) {
	page, err := h.store.LoginPage(c.Request.Context(), who.tenantID, loginQuery(c))
	writePage(c, page, err)
}

func (h *handler) loginGet(c *gin.Context, who caller) {
	item, err := h.store.LoginByID(c.Request.Context(), who.tenantID, queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) loginExport(c *gin.Context, who caller) {
	q := loginQuery(c)
	q.PageSize = -1
	page, err := h.store.LoginPage(c.Request.Context(), who.tenantID, q)
	if err != nil {
		writeAuth(c, err)
		return
	}
	rows := make([][]string, 0, len(page.List))
	for _, item := range page.List {
		rows = append(rows, []string{sheet.Cell(item.ID), sheet.Cell(int64(item.LogType)), item.Username, sheet.Cell(int64(item.Result)), item.UserIP, sheet.Cell(item.CreateTime)})
	}
	sheet.Write(c, "登录日志.xls", []string{"编号", "日志类型", "用户账号", "结果", "登录地址", "创建时间"}, rows)
}

func (h *handler) operatePage(c *gin.Context, who caller) {
	page, err := h.store.OperatePage(c.Request.Context(), who.tenantID, operateQuery(c))
	writePage(c, page, err)
}

func (h *handler) operateGet(c *gin.Context, who caller) {
	item, err := h.store.OperateByID(c.Request.Context(), who.tenantID, queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) operateExport(c *gin.Context, who caller) {
	q := operateQuery(c)
	q.PageSize = -1
	page, err := h.store.OperatePage(c.Request.Context(), who.tenantID, q)
	if err != nil {
		writeAuth(c, err)
		return
	}
	rows := make([][]string, 0, len(page.List))
	for _, item := range page.List {
		rows = append(rows, []string{sheet.Cell(item.ID), item.UserName, item.Type, item.SubType, item.Action, sheet.Cell(item.CreateTime)})
	}
	sheet.Write(c, "操作日志.xls", []string{"编号", "操作人", "操作模块", "操作名", "操作内容", "创建时间"}, rows)
}

func (h *handler) createLogin(c *gin.Context) {
	var item auth.LoginRecord
	if err := c.ShouldBindJSON(&item); err != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return
	}
	if item.TenantID == 0 {
		item.TenantID, _ = headerTenant(c)
	}
	if err := h.store.RecordLogin(c.Request.Context(), item); err != nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	httpx.OK(c, true)
}

func (h *handler) createOperate(c *gin.Context) {
	var item OperateLog
	if err := c.ShouldBindJSON(&item); err != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return
	}
	tenantID, _ := headerTenant(c)
	if err := h.store.CreateOperate(c.Request.Context(), tenantID, item); err != nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	httpx.OK(c, true)
}

func (h *handler) operateRPCPage(c *gin.Context) {
	tenantID, _ := headerTenant(c)
	page, err := h.store.OperatePage(c.Request.Context(), tenantID, operateQuery(c))
	writePage(c, page, err)
}

func loginQuery(c *gin.Context) LoginQuery {
	q := LoginQuery{
		PageNo: atoi(c.Query("pageNo"), 1), PageSize: atoi(c.Query("pageSize"), 10),
		UserIP: c.Query("userIp"), Username: c.Query("username"),
	}
	q.Start, q.End = timeRange(c, "createTime")
	switch c.Query("status") {
	case "true", "1":
		yes := true
		q.Success = &yes
	case "false", "0":
		no := false
		q.Success = &no
	}
	return q
}

func operateQuery(c *gin.Context) OperateQuery {
	q := OperateQuery{
		PageNo: atoi(c.Query("pageNo"), 1), PageSize: atoi(c.Query("pageSize"), 10),
		UserID: queryInt(c, "userId"), BizID: queryInt(c, "bizId"),
		Type: c.Query("type"), SubType: c.Query("subType"), Action: c.Query("action"),
	}
	q.Start, q.End = timeRange(c, "createTime")
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
