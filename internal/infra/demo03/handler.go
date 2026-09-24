package demo03

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

// Mount 挂上标准、内嵌和 ERP 三种学生示例。前两种随学生保存课程和班级。
// 分组路径必须写成字面量，路由对照脚本按源码字面路径计数。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	normal := r.Group("/admin-api/infra/demo03-student-normal")
	normal.POST("/create", h.permit("infra:demo03-student:create", func(c *gin.Context, tenantID int64) { h.create(c, tenantID, true) }))
	normal.PUT("/update", h.permit("infra:demo03-student:update", func(c *gin.Context, tenantID int64) { h.update(c, tenantID, true) }))
	normal.DELETE("/delete", h.permit("infra:demo03-student:delete", h.delete))
	normal.DELETE("/delete-list", h.permit("infra:demo03-student:delete", h.deleteList))
	normal.GET("/get", h.permit("infra:demo03-student:query", h.get))
	normal.GET("/page", h.permit("infra:demo03-student:query", h.page))
	normal.GET("/export-excel", h.permit("infra:demo03-student:export", h.export))
	normal.GET("/demo03-course/list-by-student-id", h.permit("infra:demo03-student:query", h.courseList))
	normal.GET("/demo03-grade/get-by-student-id", h.permit("infra:demo03-student:query", h.gradeByStudent))

	inner := r.Group("/admin-api/infra/demo03-student-inner")
	inner.POST("/create", h.permit("infra:demo03-student:create", func(c *gin.Context, tenantID int64) { h.create(c, tenantID, true) }))
	inner.PUT("/update", h.permit("infra:demo03-student:update", func(c *gin.Context, tenantID int64) { h.update(c, tenantID, true) }))
	inner.DELETE("/delete", h.permit("infra:demo03-student:delete", h.delete))
	inner.DELETE("/delete-list", h.permit("infra:demo03-student:delete", h.deleteList))
	inner.GET("/get", h.permit("infra:demo03-student:query", h.get))
	inner.GET("/page", h.permit("infra:demo03-student:query", h.page))
	inner.GET("/export-excel", h.permit("infra:demo03-student:export", h.export))
	inner.GET("/demo03-course/list-by-student-id", h.permit("infra:demo03-student:query", h.courseList))
	inner.GET("/demo03-grade/get-by-student-id", h.permit("infra:demo03-student:query", h.gradeByStudent))

	erp := r.Group("/admin-api/infra/demo03-student-erp")
	erp.POST("/create", h.permit("infra:demo03-student:create", func(c *gin.Context, tenantID int64) { h.create(c, tenantID, false) }))
	erp.PUT("/update", h.permit("infra:demo03-student:update", func(c *gin.Context, tenantID int64) { h.update(c, tenantID, false) }))
	erp.DELETE("/delete", h.permit("infra:demo03-student:delete", h.delete))
	erp.DELETE("/delete-list", h.permit("infra:demo03-student:delete", h.deleteList))
	erp.GET("/get", h.permit("infra:demo03-student:query", h.get))
	erp.GET("/page", h.permit("infra:demo03-student:query", h.page))
	erp.GET("/export-excel", h.permit("infra:demo03-student:export", h.export))
	erp.GET("/demo03-course/page", h.permit("infra:demo03-student:query", h.coursePage))
	erp.POST("/demo03-course/create", h.permit("infra:demo03-student:create", h.courseCreate))
	erp.PUT("/demo03-course/update", h.permit("infra:demo03-student:update", h.courseUpdate))
	erp.DELETE("/demo03-course/delete", h.permit("infra:demo03-student:delete", h.courseDelete))
	erp.DELETE("/demo03-course/delete-list", h.permit("infra:demo03-student:delete", h.courseDeleteList))
	erp.GET("/demo03-course/get", h.permit("infra:demo03-student:query", h.courseGet))
	erp.GET("/demo03-grade/page", h.permit("infra:demo03-student:query", h.gradePage))
	erp.POST("/demo03-grade/create", h.permit("infra:demo03-student:create", h.gradeCreate))
	erp.PUT("/demo03-grade/update", h.permit("infra:demo03-student:update", h.gradeUpdate))
	erp.DELETE("/demo03-grade/delete", h.permit("infra:demo03-student:delete", h.gradeDelete))
	erp.DELETE("/demo03-grade/delete-list", h.permit("infra:demo03-student:delete", h.gradeDeleteList))
	erp.GET("/demo03-grade/get", h.permit("infra:demo03-student:query", h.gradeGet))
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

func (h *handler) create(c *gin.Context, tenantID int64, nested bool) {
	in, ok := bindStudent(c, false, nested)
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

func (h *handler) update(c *gin.Context, tenantID int64, nested bool) {
	in, ok := bindStudent(c, true, nested)
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
	ids, ok := queryIDs(c, "ids")
	if !ok {
		return
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
	httpx.OK(c, studentJSON(*item))
}

func (h *handler) page(c *gin.Context, tenantID int64) {
	page, err := h.svc.Page(c.Request.Context(), tenantID, queryOf(c))
	if err != nil {
		writeBiz(c, err)
		return
	}
	list := make([]gin.H, 0, len(page.List))
	for _, item := range page.List {
		list = append(list, studentJSON(item))
	}
	httpx.OK(c, gin.H{"list": list, "total": page.Total})
}

func (h *handler) export(c *gin.Context, tenantID int64) {
	q := queryOf(c)
	q.PageSize = -1
	labels, err := h.svc.SexLabels(c.Request.Context())
	if err != nil {
		writeBiz(c, err)
		return
	}
	page, err := h.svc.Page(c.Request.Context(), tenantID, q)
	if err != nil {
		writeBiz(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "学生.xls", "数据", []string{"编号", "名字", "性别", "出生日期", "简介", "创建时间"},
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

func (h *handler) courseList(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "studentId")
	if !ok {
		return
	}
	list, err := h.svc.Courses(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, courseJSON(item))
	}
	httpx.OK(c, out)
}

func (h *handler) gradeByStudent(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "studentId")
	if !ok {
		return
	}
	item, err := h.svc.GradeByStudent(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if item == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, gradeJSON(*item))
}

func (h *handler) coursePage(c *gin.Context, tenantID int64) {
	studentID, ok := queryID(c, "studentId")
	if !ok {
		return
	}
	page, err := h.svc.PageCourses(c.Request.Context(), tenantID, studentID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10))
	if err != nil {
		writeBiz(c, err)
		return
	}
	list := make([]gin.H, 0, len(page.List))
	for _, item := range page.List {
		list = append(list, courseJSON(item))
	}
	httpx.OK(c, gin.H{"list": list, "total": page.Total})
}

func (h *handler) courseCreate(c *gin.Context, tenantID int64) {
	item, ok := bindCourse(c, false)
	if !ok {
		return
	}
	id, err := h.svc.CreateCourse(c.Request.Context(), tenantID, item)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, id)
}

func (h *handler) courseUpdate(c *gin.Context, tenantID int64) {
	item, ok := bindCourse(c, true)
	if !ok {
		return
	}
	if err := h.svc.UpdateCourse(c.Request.Context(), tenantID, item); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) courseDelete(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteCourse(c.Request.Context(), tenantID, id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) courseDeleteList(c *gin.Context, tenantID int64) {
	ids, ok := queryIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeleteCourseList(c.Request.Context(), tenantID, ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) courseGet(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "id")
	if !ok {
		return
	}
	item, err := h.svc.GetCourse(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if item == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, courseJSON(*item))
}

func (h *handler) gradePage(c *gin.Context, tenantID int64) {
	studentID, ok := queryID(c, "studentId")
	if !ok {
		return
	}
	page, err := h.svc.PageGrades(c.Request.Context(), tenantID, studentID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10))
	if err != nil {
		writeBiz(c, err)
		return
	}
	list := make([]gin.H, 0, len(page.List))
	for _, item := range page.List {
		list = append(list, gradeJSON(item))
	}
	httpx.OK(c, gin.H{"list": list, "total": page.Total})
}

func (h *handler) gradeCreate(c *gin.Context, tenantID int64) {
	item, ok := bindGrade(c, false)
	if !ok {
		return
	}
	id, err := h.svc.CreateGrade(c.Request.Context(), tenantID, item)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, id)
}

func (h *handler) gradeUpdate(c *gin.Context, tenantID int64) {
	item, ok := bindGrade(c, true)
	if !ok {
		return
	}
	if err := h.svc.UpdateGrade(c.Request.Context(), tenantID, item); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) gradeDelete(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteGrade(c.Request.Context(), tenantID, id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) gradeDeleteList(c *gin.Context, tenantID int64) {
	ids, ok := queryIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeleteGradeList(c.Request.Context(), tenantID, ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) gradeGet(c *gin.Context, tenantID int64) {
	id, ok := queryID(c, "id")
	if !ok {
		return
	}
	item, err := h.svc.GetGrade(c.Request.Context(), tenantID, id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if item == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, gradeJSON(*item))
}

func bindStudent(c *gin.Context, update, nested bool) (Save, bool) {
	var req struct {
		ID          *int64          `json:"id"`
		Name        *string         `json:"name"`
		Sex         *int            `json:"sex"`
		Birthday    json.RawMessage `json:"birthday"`
		Description *string         `json:"description"`
		Courses     []struct {
			ID    int64  `json:"id"`
			Name  string `json:"name"`
			Score int    `json:"score"`
		} `json:"demo03Courses"`
		Grade *struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Teacher string `json:"teacher"`
		} `json:"demo03Grade"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBiz(c, biz(400, "请求参数不正确"))
		return Save{}, false
	}
	if req.Name == nil || *req.Name == "" {
		writeBiz(c, biz(400, "名字不能为空"))
		return Save{}, false
	}
	if req.Sex == nil {
		writeBiz(c, biz(400, "性别不能为空"))
		return Save{}, false
	}
	birthday, ok := parseTime(req.Birthday)
	if !ok {
		writeBiz(c, biz(400, "出生日期不能为空"))
		return Save{}, false
	}
	if req.Description == nil || *req.Description == "" {
		writeBiz(c, biz(400, "简介不能为空"))
		return Save{}, false
	}
	in := Save{Name: *req.Name, Sex: *req.Sex, Birthday: birthday, Description: *req.Description, WithChild: nested}
	if nested {
		for _, course := range req.Courses {
			in.Courses = append(in.Courses, Course{ID: course.ID, Name: course.Name, Score: course.Score})
		}
		if req.Grade != nil {
			in.Grade = &Grade{ID: req.Grade.ID, Name: req.Grade.Name, Teacher: req.Grade.Teacher}
		}
	}
	if update {
		if req.ID == nil || *req.ID <= 0 {
			writeBiz(c, biz(codeStudentMissing, "学生不存在"))
			return Save{}, false
		}
		in.ID = *req.ID
	}
	return in, true
}

func bindCourse(c *gin.Context, update bool) (Course, bool) {
	var req struct {
		ID        *int64  `json:"id"`
		StudentID *int64  `json:"studentId"`
		Name      *string `json:"name"`
		Score     *int    `json:"score"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.StudentID == nil || req.Name == nil || req.Score == nil {
		writeBiz(c, biz(400, "请求参数不正确"))
		return Course{}, false
	}
	item := Course{StudentID: *req.StudentID, Name: *req.Name, Score: *req.Score}
	if update {
		if req.ID == nil || *req.ID <= 0 {
			writeBiz(c, biz(codeCourseMissing, "学生课程不存在"))
			return Course{}, false
		}
		item.ID = *req.ID
	}
	return item, true
}

func bindGrade(c *gin.Context, update bool) (Grade, bool) {
	var req struct {
		ID        *int64  `json:"id"`
		StudentID *int64  `json:"studentId"`
		Name      *string `json:"name"`
		Teacher   *string `json:"teacher"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.StudentID == nil || req.Name == nil || req.Teacher == nil {
		writeBiz(c, biz(400, "请求参数不正确"))
		return Grade{}, false
	}
	item := Grade{StudentID: *req.StudentID, Name: *req.Name, Teacher: *req.Teacher}
	if update {
		if req.ID == nil || *req.ID <= 0 {
			writeBiz(c, biz(codeGradeMissing, "学生班级不存在"))
			return Grade{}, false
		}
		item.ID = *req.ID
	}
	return item, true
}

func parseTime(raw json.RawMessage) (int64, bool) {
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
	return 0, false
}

func studentJSON(item Student) gin.H {
	return gin.H{"id": item.ID, "name": item.Name, "sex": item.Sex, "birthday": item.Birthday, "description": item.Description, "createTime": item.CreateTime}
}

func courseJSON(item Course) gin.H {
	return gin.H{"id": item.ID, "studentId": item.StudentID, "name": item.Name, "score": item.Score, "createTime": item.CreateTime}
}

func gradeJSON(item Grade) gin.H {
	return gin.H{"id": item.ID, "studentId": item.StudentID, "name": item.Name, "teacher": item.Teacher, "createTime": item.CreateTime}
}

func queryOf(c *gin.Context) Query {
	q := Query{
		PageNo: atoi(c.Query("pageNo"), 1), PageSize: atoi(c.Query("pageSize"), 10),
		Name: c.Query("name"), Description: c.Query("description"),
		CreateFrom: timeBound(c, 0), CreateTo: timeBound(c, 1),
	}
	if text, present := c.GetQuery("sex"); present && text != "" {
		if sex, err := strconv.Atoi(text); err == nil {
			q.Sex = &sex
		}
	}
	return q
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

func queryIDs(c *gin.Context, name string) ([]int64, bool) {
	values, present := c.Request.URL.Query()[name]
	if !present {
		writeBiz(c, biz(400, "请求参数缺失:"+name))
		return nil, false
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
				return nil, false
			}
			ids = append(ids, id)
		}
	}
	return ids, true
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
