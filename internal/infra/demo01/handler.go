package demo01

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上示例联系人。导出忽略分页上限，性别写成字典标签。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	g := r.Group("/admin-api/infra/demo01-contact")
	g.POST("/create", h.permit("infra:demo01-contact:create", h.create))
	g.PUT("/update", h.permit("infra:demo01-contact:update", h.update))
	g.DELETE("/delete", h.permit("infra:demo01-contact:delete", h.delete))
	g.DELETE("/delete-list", h.permit("infra:demo01-contact:delete", h.deleteList))
	g.GET("/get", h.permit("infra:demo01-contact:query", h.get))
	g.GET("/page", h.permit("infra:demo01-contact:query", h.page))
	g.GET("/export-excel", h.permit("infra:demo01-contact:export", h.export))
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

func (h *handler) deleteList(c *gin.Context, tenantID int64) {
	values, present := c.Request.URL.Query()["ids"]
	if !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:ids"})
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
				writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
				return
			}
			ids = append(ids, id)
		}
	}
	if err := h.svc.DeleteList(c.Request.Context(), tenantID, ids); err != nil {
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
	httpx.OK(c, contactJSON(*item))
}

func (h *handler) page(c *gin.Context, tenantID int64) {
	page, err := h.svc.Page(c.Request.Context(), tenantID, queryOf(c))
	if err != nil {
		writeBiz(c, err)
		return
	}
	list := make([]gin.H, 0, len(page.List))
	for _, item := range page.List {
		list = append(list, contactJSON(item))
	}
	httpx.OK(c, gin.H{"list": list, "total": page.Total})
}

func (h *handler) export(c *gin.Context, tenantID int64) {
	q := queryOf(c)
	q.PageSize = -1
	labels, err := h.svc.sexLabels(c.Request.Context())
	if err != nil {
		writeBiz(c, err)
		return
	}
	page, err := h.svc.Page(c.Request.Context(), tenantID, q)
	if err != nil {
		writeBiz(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "示例联系人.xls", "数据", []string{"编号", "名字", "性别", "出生年", "简介", "头像", "创建时间"},
		func(emit func([]sheet.XLSXCell) error) error {
			for _, item := range page.List {
				sex := strconv.Itoa(item.Sex)
				if text, ok := labels[sex]; ok {
					sex = text
				}
				if err := emit([]sheet.XLSXCell{
					{Value: strconv.FormatInt(item.ID, 10)},
					{Value: item.Name},
					{Value: sex},
					{Value: excelTime(item.Birthday)},
					{Value: item.Description},
					{Value: item.Avatar},
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
	q := Query{
		PageNo:     atoi(c.Query("pageNo"), 1),
		PageSize:   atoi(c.Query("pageSize"), 10),
		Name:       c.Query("name"),
		CreateFrom: timeBound(c, 0),
		CreateTo:   timeBound(c, 1),
	}
	if text, present := c.GetQuery("sex"); present && text != "" {
		if sex, err := strconv.Atoi(text); err == nil {
			q.Sex = &sex
		}
	}
	return q
}

type saveBody struct {
	ID          *int64          `json:"id"`
	Name        *string         `json:"name"`
	Sex         *int            `json:"sex"`
	Birthday    json.RawMessage `json:"birthday"`
	Description *string         `json:"description"`
	Avatar      *string         `json:"avatar"`
}

func bindSave(c *gin.Context, update bool) (Save, bool) {
	var req saveBody
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return Save{}, false
	}
	if req.Name == nil || *req.Name == "" {
		writeBiz(c, &Error{Code: 400, Msg: "名字不能为空"})
		return Save{}, false
	}
	if req.Sex == nil {
		writeBiz(c, &Error{Code: 400, Msg: "性别不能为空"})
		return Save{}, false
	}
	if len(req.Birthday) == 0 || string(req.Birthday) == "null" {
		writeBiz(c, &Error{Code: 400, Msg: "出生年不能为空"})
		return Save{}, false
	}
	birthday, ok := parseBirthday(req.Birthday)
	if !ok {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return Save{}, false
	}
	if req.Description == nil || *req.Description == "" {
		writeBiz(c, &Error{Code: 400, Msg: "简介不能为空"})
		return Save{}, false
	}
	in := Save{Name: *req.Name, Sex: *req.Sex, Birthday: birthday, Description: *req.Description, Avatar: req.Avatar}
	if update {
		if req.ID == nil || *req.ID <= 0 {
			writeBiz(c, missing())
			return Save{}, false
		}
		in.ID = *req.ID
	}
	return in, true
}

func parseBirthday(raw json.RawMessage) (int64, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return 0, false
	}
	if text[0] != '"' {
		n, err := strconv.ParseInt(text, 10, 64)
		return n, err == nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return n, true
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", value, shanghai()); err == nil {
		return t.UnixMilli(), true
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UnixMilli(), true
	}
	return 0, false
}

func contactJSON(item Contact) gin.H {
	avatar := any(nil)
	if item.Avatar != "" {
		avatar = item.Avatar
	}
	return gin.H{
		"id": item.ID, "name": item.Name, "sex": item.Sex, "birthday": item.Birthday,
		"description": item.Description, "avatar": avatar, "createTime": item.CreateTime,
	}
}

func excelTime(millis int64) string {
	if millis <= 0 {
		return ""
	}
	return time.UnixMilli(millis).In(shanghai()).Format("2006-01-02 15:04:05")
}

func queryID(c *gin.Context, name string) (int64, bool) {
	if _, present := c.GetQuery(name); !present {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数缺失:" + name})
		return 0, false
	}
	id, err := strconv.ParseInt(c.Query(name), 10, 64)
	if err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数类型错误:" + name})
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
	httpx.Fail(c, http.StatusOK, 401, "账号未登录")
}
