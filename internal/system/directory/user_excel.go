package directory

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
)

func (h *handler) userList(c *gin.Context, who caller) {
	if _, present := c.GetQuery("ids"); !present {
		httpx.Fail(c, http.StatusOK, 400, "请求参数缺失:ids")
		return
	}
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	list, err := h.svc.UserListByIDs(c.Request.Context(), who.tenantID, ids)
	writeList(c, list, err)
}

func (h *handler) userSimpleOne(c *gin.Context, who caller) {
	if _, present := c.GetQuery("id"); !present {
		httpx.Fail(c, http.StatusOK, 400, "请求参数缺失:id")
		return
	}
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求参数类型错误:id")
		return
	}
	user, err := h.svc.UserGet(c.Request.Context(), who.tenantID, id)
	if err != nil {
		writeErr(c, err)
		return
	}
	if user == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, UserSimple{ID: user.ID, Nickname: user.Nickname, Avatar: user.Avatar, Sex: user.Sex, DeptID: user.DeptID, DeptName: user.DeptName})
}

func (h *handler) userByNickname(c *gin.Context, who caller) {
	if _, present := c.GetQuery("nickname"); !present {
		httpx.Fail(c, http.StatusOK, 400, "请求参数缺失:nickname")
		return
	}
	nickname := strings.TrimSpace(c.Query("nickname"))
	if nickname == "" {
		httpx.OK(c, []UserSimple{})
		return
	}
	list, err := h.svc.UserByNickname(c.Request.Context(), who.tenantID, nickname)
	writeList(c, list, err)
}

func (h *handler) userExport(c *gin.Context, who caller) {
	query := UserQuery{Username: c.Query("username"), Mobile: c.Query("mobile")}
	if err := fillUserQuery(c, &query); err != nil {
		writeErr(c, err)
		return
	}
	sexLabels, err := h.svc.DictLabels(c.Request.Context(), "system_user_sex")
	if err != nil {
		writeErr(c, err)
		return
	}
	statusLabels, err := h.svc.DictLabels(c.Request.Context(), "common_status")
	if err != nil {
		writeErr(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "用户数据.xls", "数据", []string{"用户编号", "用户名称", "用户昵称", "部门名称", "用户邮箱", "手机号码", "用户性别", "帐号状态", "最后登录IP", "最后登录时间"},
		func(emit func([]sheet.XLSXCell) error) error {
			return h.svc.UserExportRows(c.Request.Context(), who.tenantID, query, func(user UserDetail) error {
				return emit([]sheet.XLSXCell{
					{Value: strconv.FormatInt(user.ID, 10)},
					{Value: user.Username},
					{Value: user.Nickname},
					{Value: user.DeptName},
					{Value: user.Email},
					{Value: user.Mobile},
					{Value: dictLabel(sexLabels, user.Sex)},
					{Value: dictLabelInt(statusLabels, user.Status)},
					{Value: user.LoginIP},
					{Value: excelTime(user.LoginDate)},
				})
			})
		})
	if err != nil {
		writeErr(c, err)
	}
}

func (h *handler) roleExport(c *gin.Context, who caller) {
	var status *int
	if text := c.Query("status"); text != "" {
		value := atoi(text, 0)
		status = &value
	}
	statusLabels, err := h.svc.DictLabels(c.Request.Context(), "common_status")
	if err != nil {
		writeErr(c, err)
		return
	}
	scopeLabels, err := h.svc.DictLabels(c.Request.Context(), "system_data_scope")
	if err != nil {
		writeErr(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "角色数据.xls", "数据", []string{"角色序号", "角色名称", "角色标志", "角色排序", "角色状态", "数据范围"},
		func(emit func([]sheet.XLSXCell) error) error {
			return h.svc.RoleExportRows(c.Request.Context(), who.tenantID, c.Query("name"), c.Query("code"), status, func(role RoleDetail) error {
				return emit([]sheet.XLSXCell{
					{Value: strconv.FormatInt(role.ID, 10)},
					{Value: role.Name},
					{Value: role.Code},
					{Value: strconv.Itoa(role.Sort), Numeric: true},
					{Value: dictLabelInt(statusLabels, role.Status)},
					{Value: dictLabelInt(scopeLabels, role.DataScope)},
				})
			})
		})
	if err != nil {
		writeErr(c, err)
	}
}

func (h *handler) userImportTemplate(c *gin.Context, _ caller) {
	sexLabels, err := h.svc.DictLabels(c.Request.Context(), "system_user_sex")
	if err != nil {
		writeErr(c, err)
		return
	}
	statusLabels, err := h.svc.DictLabels(c.Request.Context(), "common_status")
	if err != nil {
		writeErr(c, err)
		return
	}
	male, female := 1, 2
	enable, disable := 0, 1
	err = sheet.WriteXLSXStream(c, "用户导入模板.xls", "用户列表", importHeaders, func(emit func([]sheet.XLSXCell) error) error {
		// 模板保留字段和格式示例；手机号留空，避免把看似真实的号码发给使用者。
		if err := emit(importCells("yunai", "芋道", "1", "yunai@example.com", "", dictLabel(sexLabels, &male), dictLabelInt(statusLabels, enable))); err != nil {
			return err
		}
		return emit(importCells("yuanma", "源码", "2", "yuanma@example.com", "", dictLabel(sexLabels, &female), dictLabelInt(statusLabels, disable)))
	})
	if err != nil {
		writeErr(c, err)
	}
}

func (h *handler) userImport(c *gin.Context, who caller) {
	file, err := c.FormFile("file")
	if err != nil {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "请求参数不正确"})
		return
	}
	if file.Size > 16<<20 {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "请求参数不正确"})
		return
	}
	opened, err := file.Open()
	if err != nil {
		writeErr(c, err)
		return
	}
	defer opened.Close()
	raw, err := io.ReadAll(io.LimitReader(opened, 16<<20+1))
	if err != nil {
		writeErr(c, err)
		return
	}
	grid, err := sheet.ReadXLSX(raw)
	if err != nil {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "请求参数不正确"})
		return
	}
	sexLabels, err := h.svc.DictLabels(c.Request.Context(), "system_user_sex")
	if err != nil {
		writeErr(c, err)
		return
	}
	statusLabels, err := h.svc.DictLabels(c.Request.Context(), "common_status")
	if err != nil {
		writeErr(c, err)
		return
	}
	updateSupport, _ := strconv.ParseBool(c.PostForm("updateSupport"))
	result, err := h.svc.ImportUsers(c.Request.Context(), who.tenantID, parseImportRows(grid, invertLabels(sexLabels), invertLabels(statusLabels)), updateSupport)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, result)
}

var importHeaders = []string{"登录名称", "用户名称", "部门编号", "用户邮箱", "手机号码", "用户性别", "账号状态"}

func importCells(username, nickname, dept, email, mobile, sex, status string) []sheet.XLSXCell {
	return []sheet.XLSXCell{
		{Value: username}, {Value: nickname}, {Value: dept}, {Value: email}, {Value: mobile}, {Value: sex}, {Value: status},
	}
}

func parseImportRows(grid [][]string, sex, status map[string]int) []ImportUser {
	if len(grid) <= 1 {
		return nil
	}
	index := map[string]int{}
	for i, name := range grid[0] {
		index[strings.TrimSpace(name)] = i
	}
	rows := make([]ImportUser, 0, len(grid)-1)
	for _, line := range grid[1:] {
		row := ImportUser{
			Username: cell(line, index, "登录名称"),
			Nickname: cell(line, index, "用户名称"),
			Email:    cell(line, index, "用户邮箱"),
			Mobile:   cell(line, index, "手机号码"),
		}
		if text := cell(line, index, "部门编号"); text != "" {
			if id, err := strconv.ParseInt(text, 10, 64); err == nil {
				row.DeptID = &id
			}
		}
		if text := cell(line, index, "用户性别"); text != "" {
			if value, ok := sex[text]; ok {
				row.Sex = &value
			}
		}
		if text := cell(line, index, "账号状态"); text != "" {
			if value, ok := status[text]; ok {
				row.Status = &value
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func cell(line []string, index map[string]int, name string) string {
	pos, ok := index[name]
	if !ok || pos < 0 || pos >= len(line) {
		return ""
	}
	return strings.TrimSpace(line[pos])
}

func invertLabels(labels map[int]string) map[string]int {
	out := make(map[string]int, len(labels))
	for value, label := range labels {
		if _, seen := out[label]; !seen {
			out[label] = value
		}
	}
	return out
}

func dictLabel(labels map[int]string, value *int) string {
	if value == nil {
		return ""
	}
	return dictLabelInt(labels, *value)
}

func dictLabelInt(labels map[int]string, value int) string {
	if text, ok := labels[value]; ok {
		return text
	}
	return ""
}
