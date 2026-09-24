package directory

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// websitePattern 对齐 App 按域名查租户的校验：主机名可带端口，不能带路径。
var websitePattern = regexp.MustCompile(`^[a-zA-Z0-9.-]+(:\d{1,5})?$`)

// Mount 挂上管理后台打开后就会请求的列表，以及用户的增删改。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	dict := r.Group("/admin-api/system/dict-data")
	dict.GET("/simple-list", h.login(h.dictSimple))
	dict.GET("/list-all-simple", h.login(h.dictSimple))
	dict.GET("/page", h.permit("system:dict:query", h.dictPage))
	dict.GET("/get", h.permit("system:dict:query", h.dictDataGet))
	dict.POST("/create", h.permit("system:dict:create", h.dictDataSave))
	dict.PUT("/update", h.permit("system:dict:update", h.dictDataSave))
	dict.DELETE("/delete", h.permit("system:dict:delete", h.dictDataDelete))
	dict.DELETE("/delete-list", h.permit("system:dict:delete", h.dictDataDeleteList))
	dict.GET("/export-excel", h.permit("system:dict:export", h.dictDataExport))

	dictType := r.Group("/admin-api/system/dict-type")
	dictType.GET("/simple-list", h.login(h.dictTypeSimple))
	dictType.GET("/list-all-simple", h.login(h.dictTypeSimple))
	dictType.GET("/page", h.permit("system:dict:query", h.dictTypePage))
	dictType.GET("/get", h.permit("system:dict:query", h.dictTypeGet))
	dictType.POST("/create", h.permit("system:dict:create", h.dictTypeSave))
	dictType.PUT("/update", h.permit("system:dict:update", h.dictTypeSave))
	dictType.DELETE("/delete", h.permit("system:dict:delete", h.dictTypeDelete))
	dictType.DELETE("/delete-list", h.permit("system:dict:delete", h.dictTypeDeleteList))
	// Java v2026.08 的类型导出使用 query 权限，数据导出才使用 export 权限。
	dictType.GET("/export-excel", h.permit("system:dict:query", h.dictTypeExport))

	dept := r.Group("/admin-api/system/dept")
	dept.GET("/list", h.permit("system:dept:query", h.scoped(h.deptList)))
	dept.GET("/simple-list", h.login(h.scoped(h.deptSimple)))
	dept.GET("/list-all-simple", h.login(h.scoped(h.deptSimple)))
	dept.POST("/create", h.permit("system:dept:create", h.deptSave))
	dept.PUT("/update", h.permit("system:dept:update", h.deptSave))
	MountDeptExtras(r, sessions, svc)

	user := r.Group("/admin-api/system/user")
	user.GET("/simple-list", h.login(h.scoped(h.userSimple)))
	user.GET("/list-all-simple", h.login(h.scoped(h.userSimple)))
	user.GET("/page", h.permit("system:user:query", h.scoped(h.userPage)))
	user.GET("/get", h.permit("system:user:query", h.scoped(h.userGet)))
	user.POST("/create", h.permit("system:user:create", h.userCreate))
	user.PUT("/update", h.permit("system:user:update", h.scoped(h.userUpdate)))
	user.PUT("/update-password", h.permit("system:user:update-password", h.scoped(h.userPassword)))
	user.PUT("/update-status", h.permit("system:user:update", h.scoped(h.userStatus)))
	user.DELETE("/delete", h.permit("system:user:delete", h.scoped(h.userDelete)))
	user.DELETE("/delete-list", h.permit("system:user:delete", h.scoped(h.userDeleteList)))
	user.GET("/list", h.permit("system:user:query", h.scoped(h.userList)))
	user.GET("/export-excel", h.permit("system:user:export", h.scoped(h.userExport)))
	user.GET("/get-import-template", h.login(h.userImportTemplate))
	user.POST("/import", h.permit("system:user:import", h.userImport))
	user.GET("/get-simple", h.login(h.scoped(h.userSimpleOne)))
	user.GET("/list-by-nickname", h.login(h.scoped(h.userByNickname)))

	menu := r.Group("/admin-api/system/menu")
	menu.GET("/simple-list", h.login(h.menuSimple))
	menu.GET("/list-all-simple", h.login(h.menuSimple))
	menu.GET("/list", h.permit("system:menu:query", h.menuList))
	menu.GET("/get", h.permit("system:menu:query", h.menuGet))
	menu.POST("/create", h.permit("system:menu:create", h.menuSave))
	menu.PUT("/update", h.permit("system:menu:update", h.menuSave))
	menu.DELETE("/delete", h.permit("system:menu:delete", h.menuDelete))
	menu.DELETE("/delete-list", h.permit("system:menu:delete", h.menuDeleteList))

	role := r.Group("/admin-api/system/role")
	role.GET("/simple-list", h.login(h.roleSimple))
	role.GET("/list-all-simple", h.login(h.roleSimple))
	role.GET("/page", h.permit("system:role:query", h.rolePage))
	role.GET("/get", h.permit("system:role:query", h.roleGet))
	role.POST("/create", h.permit("system:role:create", h.roleSave))
	role.PUT("/update", h.permit("system:role:update", h.roleSave))
	role.DELETE("/delete", h.permit("system:role:delete", h.roleDelete))
	role.DELETE("/delete-list", h.permit("system:role:delete", h.roleDeleteList))
	role.GET("/export-excel", h.permit("system:role:export", h.roleExport))

	post := r.Group("/admin-api/system/post")
	post.GET("/simple-list", h.login(h.postSimple))
	post.GET("/list-all-simple", h.login(h.postSimple))
	post.GET("/page", h.permit("system:post:query", h.postPage))
	post.GET("/get", h.permit("system:post:query", h.postGet))
	post.POST("/create", h.permit("system:post:create", h.postSave))
	post.PUT("/update", h.permit("system:post:update", h.postSave))
	post.DELETE("/delete", h.permit("system:post:delete", h.postDelete))
	post.DELETE("/delete-list", h.permit("system:post:delete", h.postDeleteList))
	post.GET("/export-excel", h.permit("system:post:export", h.postExport))

	perm := r.Group("/admin-api/system/permission")
	perm.GET("/list-role-menus", h.permit("system:permission:assign-role-menu", h.roleMenus))
	perm.POST("/assign-role-menu", h.permit("system:permission:assign-role-menu", h.assignRoleMenus))
	perm.POST("/assign-role-data-scope", h.permit("system:permission:assign-role-data-scope", h.assignRoleDataScope))
	perm.GET("/list-user-roles", h.permit("system:permission:assign-user-role", h.userRoles))
	perm.POST("/assign-user-role", h.permit("system:permission:assign-user-role", h.assignUserRoles))

	// 登录页用租户名或域名换编号，这三个接口不校验登录。
	tenant := r.Group("/admin-api/system/tenant")
	tenant.GET("/get-id-by-name", h.tenantIDByName)
	tenant.GET("/simple-list", h.tenantSimple)
	tenant.GET("/get-by-website", h.tenantByWebsite)
	appTenant := r.Group("/app-api/system/tenant")
	appTenant.GET("/get-by-website", h.appTenantByWebsite)
	appDict := r.Group("/app-api/system/dict-data")
	appDict.GET("/type", h.appDictByType)
	tenant.GET("/page", h.permit("system:tenant:query", h.tenantPage))
	tenant.GET("/export-excel", h.permit("system:tenant:export", h.tenantExport))
	tenant.GET("/get", h.permit("system:tenant:query", h.tenantGet))
	tenant.POST("/create", h.permit("system:tenant:create", h.tenantSave))
	tenant.PUT("/update", h.permit("system:tenant:update", h.tenantSave))
	tenant.DELETE("/delete", h.permit("system:tenant:delete", h.tenantDelete))
	tenant.DELETE("/delete-list", h.permit("system:tenant:delete", h.tenantDeleteList))

	pkg := r.Group("/admin-api/system/tenant-package")
	pkg.GET("/simple-list", h.login(h.packageSimple))
	pkg.GET("/get-simple-list", h.login(h.packageSimple))
	pkg.GET("/page", h.permit("system:tenant-package:query", h.packagePage))
	pkg.GET("/get", h.permit("system:tenant-package:query", h.packageGet))
	pkg.POST("/create", h.permit("system:tenant-package:create", h.packageSave))
	pkg.PUT("/update", h.permit("system:tenant-package:update", h.packageSave))
	pkg.DELETE("/delete", h.permit("system:tenant-package:delete", h.packageDelete))
	pkg.DELETE("/delete-list", h.permit("system:tenant-package:delete", h.packageDeleteList))

	notice := r.Group("/admin-api/system/notice")
	notice.GET("/page", h.permit("system:notice:query", h.noticePage))
	notice.GET("/get", h.permit("system:notice:query", h.noticeGet))
	notice.POST("/create", h.permit("system:notice:create", h.noticeSave))
	notice.PUT("/update", h.permit("system:notice:update", h.noticeSave))
	notice.DELETE("/delete", h.permit("system:notice:delete", h.noticeDelete))
	notice.DELETE("/delete-list", h.permit("system:notice:delete", h.noticeDeleteList))
	notice.POST("/push", h.permit("system:notice:update", h.noticePush))

	profile := r.Group("/admin-api/system/user/profile")
	profile.GET("/get", h.login(h.profileGet))
	profile.PUT("/update", h.login(h.profileUpdate))
	profile.PUT("/update-password", h.login(h.profilePassword))

	oauthUser := r.Group("/admin-api/system/oauth2/user")
	oauthUser.GET("/get", h.oauthUserGet)
	oauthUser.PUT("/update", h.oauthUserUpdate)
}

type handler struct {
	sessions *auth.Service
	svc      *Service
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
			writeErr(c, err)
			return
		}
		header, ok := headerTenant(c)
		if !ok {
			writeErr(c, &Error{Code: 400, Msg: "请求的租户标识未传递，请进行排查"})
			return
		}
		if header != tenantID {
			writeErr(c, &Error{Code: 403, Msg: "您无权访问该租户的数据"})
			return
		}
		if perm != "" && !perms[perm] {
			writeErr(c, &Error{Code: 403, Msg: "没有该操作权限"})
			return
		}
		next(c, caller{userID: userID, tenantID: tenantID})
	}
}

func (h *handler) dictSimple(c *gin.Context, _ caller) {
	list, err := h.svc.DictSimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) deptList(c *gin.Context, who caller) {
	list, err := h.svc.DeptList(c.Request.Context(), who.tenantID)
	writeList(c, list, err)
}

func (h *handler) deptSimple(c *gin.Context, who caller) {
	list, err := h.svc.DeptList(c.Request.Context(), who.tenantID)
	if err != nil {
		writeErr(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, dept := range list {
		if dept.Status != 0 {
			continue
		}
		out = append(out, gin.H{"id": dept.ID, "name": dept.Name, "parentId": dept.ParentID})
	}
	httpx.OK(c, out)
}

func (h *handler) deptSave(c *gin.Context, who caller) {
	var dept Dept
	if err := c.ShouldBindJSON(&dept); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		dept.ID = 0
	}
	id, err := h.svc.SaveDept(c.Request.Context(), who.tenantID, dept)
	if err != nil {
		writeErr(c, err)
		return
	}
	if c.Request.Method == http.MethodPost {
		httpx.OK(c, id)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) dictTypeSimple(c *gin.Context, _ caller) {
	list, err := h.svc.Dicts.DictTypeSimple(c.Request.Context())
	if err != nil {
		writeErr(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, gin.H{"id": item.ID, "name": item.Name, "type": item.Type})
	}
	httpx.OK(c, out)
}

func (h *handler) dictTypePage(c *gin.Context, _ caller) {
	query, ok := dictTypeQuery(c)
	if !ok {
		return
	}
	page, err := h.svc.Dicts.DictTypePage(c.Request.Context(), query)
	writePage(c, page, err)
}

func (h *handler) dictTypeSave(c *gin.Context, _ caller) {
	var item DictType
	if err := c.ShouldBindJSON(&item); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveDictType(c.Request.Context(), item)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) dictTypeDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteDictType(c.Request.Context(), int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) dictDataSave(c *gin.Context, _ caller) {
	var item DictData
	if err := c.ShouldBindJSON(&item); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveDictData(c.Request.Context(), item)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) dictDataDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteDictData(c.Request.Context(), int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) dictPage(c *gin.Context, _ caller) {
	pageNo, pageSize, status, ok := dictPageQuery(c)
	if !ok || !dictDataFieldsValid(c) {
		return
	}
	page, err := h.svc.Dicts.DictDataPage(c.Request.Context(), DictDataQuery{
		PageNo: pageNo, PageSize: pageSize, Label: c.Query("label"), DictType: c.Query("dictType"), Status: status,
	})
	writePage(c, page, err)
}

func (h *handler) menuList(c *gin.Context, _ caller) {
	var status *int
	if text := c.Query("status"); text != "" {
		value := atoi(text, 0)
		status = &value
	}
	list, err := h.svc.MenuList(c.Request.Context(), c.Query("name"), status)
	writeList(c, list, err)
}

func (h *handler) menuGet(c *gin.Context, _ caller) {
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	item, err := h.svc.Access.MenuByID(c.Request.Context(), id)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) rolePage(c *gin.Context, who caller) {
	var status *int
	if text := c.Query("status"); text != "" {
		value := atoi(text, 0)
		status = &value
	}
	page, err := h.svc.RolePage(c.Request.Context(), who.tenantID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), c.Query("code"), status)
	writePage(c, page, err)
}

func (h *handler) roleGet(c *gin.Context, who caller) {
	id, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "角色编号不正确"})
		return
	}
	role, err := h.svc.RoleGet(c.Request.Context(), who.tenantID, id)
	if err != nil {
		writeErr(c, err)
		return
	}
	// Java getRole 对不存在（包括其他租户）的角色返回 data: null。
	httpx.OK(c, role)
}

func (h *handler) postSimple(c *gin.Context, who caller) {
	list, err := h.svc.PostSimple(c.Request.Context(), who.tenantID)
	writeList(c, list, err)
}

func (h *handler) postPage(c *gin.Context, who caller) {
	query, ok := postQuery(c)
	if !ok {
		return
	}
	page, err := h.svc.PostPage(c.Request.Context(), who.tenantID, query)
	writePage(c, page, err)
}

func (h *handler) postGet(c *gin.Context, who caller) {
	id, ok := rpcID(c)
	if !ok {
		return
	}
	post, err := h.svc.PostGet(c.Request.Context(), who.tenantID, id)
	if err != nil {
		writeErr(c, err)
		return
	}
	// Java getPost 对不存在或其他租户岗位返回 data:null。
	httpx.OK(c, post)
}

// postQuery 在导出设置 PageSize=-1 之前按 Java PageParam 校验原请求。
func postQuery(c *gin.Context) (PostQuery, bool) {
	query := PostQuery{PageNo: 1, PageSize: 10, Code: c.Query("code"), Name: c.Query("name")}
	if raw, present := c.GetQuery("pageNo"); present {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "页码最小值为 1"})
			return PostQuery{}, false
		}
		query.PageNo = value
	}
	if raw, present := c.GetQuery("pageSize"); present {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "每页条数必须在 1 到 200 之间"})
			return PostQuery{}, false
		}
		query.PageSize = value
	}
	if raw := c.Query("status"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "岗位状态不正确"})
			return PostQuery{}, false
		}
		query.Status = &value
	}
	return query, true
}

func (h *handler) roleSave(c *gin.Context, who caller) {
	var role RoleSave
	if err := c.ShouldBindJSON(&role); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		role.ID = 0
	}
	id, err := h.svc.SaveRole(c.Request.Context(), who.tenantID, role)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) roleDelete(c *gin.Context, who caller) {
	if err := h.svc.DeleteRole(c.Request.Context(), who.tenantID, int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) roleDeleteList(c *gin.Context, who caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeleteRoleList(c.Request.Context(), who.tenantID, ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) menuSave(c *gin.Context, _ caller) {
	var menu MenuSave
	if err := c.ShouldBindJSON(&menu); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		menu.ID = 0
	}
	id, err := h.svc.SaveMenu(c.Request.Context(), menu)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) menuDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteMenu(c.Request.Context(), int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) menuDeleteList(c *gin.Context, _ caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeleteMenuList(c.Request.Context(), ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) postSave(c *gin.Context, who caller) {
	var post PostSave
	if err := c.ShouldBindJSON(&post); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		post.ID = 0
	}
	id, err := h.svc.SavePost(c.Request.Context(), who.tenantID, post)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) postDelete(c *gin.Context, who caller) {
	if err := h.svc.DeletePost(c.Request.Context(), who.tenantID, int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) postDeleteList(c *gin.Context, who caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeletePostList(c.Request.Context(), who.tenantID, ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) postExport(c *gin.Context, who caller) {
	query, ok := postQuery(c)
	if !ok {
		return
	}
	query.PageSize = -1
	labels, err := h.svc.PostStatusLabels(c.Request.Context())
	if err != nil {
		writeErr(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "岗位数据.xls", "岗位列表", []string{"岗位序号", "岗位名称", "岗位编码", "岗位排序", "状态"},
		func(emit func([]sheet.XLSXCell) error) error {
			return h.svc.PostExportRows(c.Request.Context(), who.tenantID, query, func(item PostDetail) error {
				// id 作为文本保留 Long 精度；sort 仍为 Excel 数值单元格。
				return emit([]sheet.XLSXCell{
					{Value: strconv.FormatInt(item.ID, 10)},
					{Value: item.Name},
					{Value: item.Code},
					{Value: strconv.Itoa(item.Sort), Numeric: true},
					{Value: labels[item.Status]},
				})
			})
		})
	if err != nil {
		writeErr(c, err)
	}
}

func (h *handler) roleMenus(c *gin.Context, who caller) {
	ids, err := h.svc.Access.RoleMenuIDs(c.Request.Context(), who.tenantID, int64(atoi(c.Query("roleId"), 0)))
	writeList(c, ids, err)
}

func (h *handler) assignRoleMenus(c *gin.Context, who caller) {
	var req struct {
		RoleID  int64   `json:"roleId"`
		MenuIDs []int64 `json:"menuIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.AssignRoleMenus(c.Request.Context(), who.tenantID, req.RoleID, req.MenuIDs); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) assignRoleDataScope(c *gin.Context, who caller) {
	var req struct {
		RoleID           *int64  `json:"roleId"`
		DataScope        *int    `json:"dataScope"`
		DataScopeDeptIDs []int64 `json:"dataScopeDeptIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.RoleID == nil || req.DataScope == nil {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.AssignRoleDataScope(c.Request.Context(), who.tenantID, *req.RoleID, *req.DataScope, req.DataScopeDeptIDs); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) userRoles(c *gin.Context, who caller) {
	ids, err := h.svc.Access.UserRoleIDs(c.Request.Context(), who.tenantID, int64(atoi(c.Query("userId"), 0)))
	writeList(c, ids, err)
}

func (h *handler) assignUserRoles(c *gin.Context, who caller) {
	var req struct {
		UserID  int64   `json:"userId"`
		RoleIDs []int64 `json:"roleIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.AssignUserRoles(c.Request.Context(), who.tenantID, req.UserID, req.RoleIDs); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func writeIDOrBool(c *gin.Context, created bool, id int64, err error) {
	if err != nil {
		writeErr(c, err)
		return
	}
	if created {
		httpx.OK(c, id)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) userSimple(c *gin.Context, who caller) {
	list, err := h.svc.UserSimple(c.Request.Context(), who.tenantID)
	writeList(c, list, err)
}

func (h *handler) scoped(next func(*gin.Context, caller)) func(*gin.Context, caller) {
	return func(c *gin.Context, who caller) {
		if h.svc.DeptScope == nil {
			writeErr(c, &Error{Code: 500, Msg: "系统异常"})
			return
		}
		access, err := h.svc.DeptScope(c.Request.Context(), who.tenantID, who.userID)
		if err != nil {
			writeErr(c, err)
			return
		}
		access.UserID = who.userID
		c.Request = c.Request.WithContext(withUserAccess(c.Request.Context(), access))
		next(c, who)
	}
}

func fillUserQuery(c *gin.Context, query *UserQuery) error {
	if text := c.Query("status"); text != "" {
		status := atoi(text, 0)
		query.Status = &status
	}
	if text := c.Query("deptId"); text != "" {
		id := int64(atoi(text, 0))
		query.DeptID = &id
	}
	if text := c.Query("roleId"); text != "" {
		id := int64(atoi(text, 0))
		query.RoleID = &id
	}
	times := c.QueryArray("createTime")
	if len(times) == 0 {
		if value, ok := c.GetQuery("createTime[0]"); ok {
			times = append(times, value)
		}
		if value, ok := c.GetQuery("createTime[1]"); ok {
			times = append(times, value)
		}
	}
	if len(times) > 2 {
		return &Error{Code: 400, Msg: "请求参数不正确"}
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	parse := func(text string) (*time.Time, error) {
		if text == "" {
			return nil, nil
		}
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", text, loc)
		if err != nil {
			return nil, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		return &parsed, nil
	}
	if len(times) >= 1 {
		query.CreatedFrom, err = parse(times[0])
		if err != nil {
			return err
		}
	}
	if len(times) >= 2 {
		query.CreatedTo, err = parse(times[1])
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) userPage(c *gin.Context, who caller) {
	query := UserQuery{
		PageNo:   atoi(c.Query("pageNo"), 1),
		PageSize: atoi(c.Query("pageSize"), 10),
		Username: c.Query("username"),
		Mobile:   c.Query("mobile"),
	}
	if err := fillUserQuery(c, &query); err != nil {
		writeErr(c, err)
		return
	}
	page, err := h.svc.UserPage(c.Request.Context(), who.tenantID, query)
	if err != nil {
		writeErr(c, err)
		return
	}
	if page.List == nil {
		page.List = []UserDetail{}
	}
	httpx.OK(c, page)
}

func (h *handler) userGet(c *gin.Context, who caller) {
	user, err := h.svc.UserGet(c.Request.Context(), who.tenantID, int64(atoi(c.Query("id"), 0)))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, user)
}

func (h *handler) userCreate(c *gin.Context, who caller) {
	var user UserSave
	if err := c.ShouldBindJSON(&user); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	id, err := h.svc.CreateUser(c.Request.Context(), who.tenantID, user)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, id)
}

func (h *handler) userUpdate(c *gin.Context, who caller) {
	var user UserSave
	if err := c.ShouldBindJSON(&user); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.UpdateUser(c.Request.Context(), who.tenantID, user); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) userPassword(c *gin.Context, who caller) {
	var req struct {
		ID       int64  `json:"id"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), who.tenantID, req.ID, req.Password); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) userStatus(c *gin.Context, who caller) {
	var req struct {
		ID     int64 `json:"id"`
		Status int   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), who.tenantID, req.ID, req.Status); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) userDelete(c *gin.Context, who caller) {
	id := int64(atoi(c.Query("id"), 0))
	if err := h.svc.DeleteUser(c.Request.Context(), who.tenantID, who.userID, id); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) userDeleteList(c *gin.Context, who caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeleteUserList(c.Request.Context(), who.tenantID, ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) menuSimple(c *gin.Context, _ caller) {
	list, err := h.svc.MenuSimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) roleSimple(c *gin.Context, who caller) {
	list, err := h.svc.RoleSimple(c.Request.Context(), who.tenantID)
	writeList(c, list, err)
}

func (h *handler) tenantIDByName(c *gin.Context) {
	item, err := h.svc.Tenants.TenantByName(c.Request.Context(), c.Query("name"))
	if err != nil {
		writeErr(c, err)
		return
	}
	if item == nil {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, item.ID)
}

func (h *handler) tenantSimple(c *gin.Context) {
	list, err := h.svc.Tenants.TenantSimple(c.Request.Context())
	if err != nil {
		writeErr(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, gin.H{"id": item.ID, "name": item.Name})
	}
	httpx.OK(c, out)
}

func (h *handler) tenantByWebsite(c *gin.Context) {
	h.writeTenantByWebsite(c, c.Query("website"))
}

func (h *handler) appTenantByWebsite(c *gin.Context) {
	website := c.Query("website")
	if !websitePattern.MatchString(website) {
		writeErr(c, &Error{Code: 400, Msg: "网站域名格式不正确"})
		return
	}
	h.writeTenantByWebsite(c, website)
}

func (h *handler) writeTenantByWebsite(c *gin.Context, website string) {
	item, err := h.svc.Tenants.TenantByWebsite(c.Request.Context(), website)
	if err != nil {
		writeErr(c, err)
		return
	}
	if item == nil || item.Status != 0 {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, gin.H{"id": item.ID, "name": item.Name})
}

func (h *handler) appDictByType(c *gin.Context) {
	dictType := c.Query("type")
	if dictType == "" {
		writeErr(c, &Error{Code: 400, Msg: "请求参数缺失:type"})
		return
	}
	list, err := h.svc.DictEnabledByType(c.Request.Context(), dictType)
	if err != nil {
		writeErr(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, gin.H{"id": item.ID, "label": item.Label, "value": item.Value, "dictType": item.DictType})
	}
	httpx.OK(c, out)
}

func (h *handler) tenantPage(c *gin.Context, _ caller) {
	page, err := h.svc.Tenants.TenantPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), c.Query("contactName"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) tenantGet(c *gin.Context, _ caller) {
	item, err := h.svc.Tenants.TenantByID(c.Request.Context(), int64(atoi(c.Query("id"), 0)))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) tenantSave(c *gin.Context, _ caller) {
	var item Tenant
	if err := c.ShouldBindJSON(&item); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveTenant(c.Request.Context(), item)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) tenantDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteTenant(c.Request.Context(), int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) tenantDeleteList(c *gin.Context, _ caller) {
	for _, id := range queryIDList(c) {
		if err := h.svc.DeleteTenant(c.Request.Context(), id); err != nil {
			writeErr(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

func (h *handler) packageSimple(c *gin.Context, _ caller) {
	list, err := h.svc.Tenants.PackageSimple(c.Request.Context())
	if err != nil {
		writeErr(c, err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, gin.H{"id": item.ID, "name": item.Name})
	}
	httpx.OK(c, out)
}

func (h *handler) packagePage(c *gin.Context, _ caller) {
	page, err := h.svc.Tenants.PackagePage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) packageGet(c *gin.Context, _ caller) {
	item, err := h.svc.Tenants.PackageByID(c.Request.Context(), int64(atoi(c.Query("id"), 0)))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) packageSave(c *gin.Context, _ caller) {
	var item TenantPackage
	if err := c.ShouldBindJSON(&item); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SavePackage(c.Request.Context(), item)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) packageDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeletePackage(c.Request.Context(), int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) packageDeleteList(c *gin.Context, _ caller) {
	for _, id := range queryIDList(c) {
		if err := h.svc.DeletePackage(c.Request.Context(), id); err != nil {
			writeErr(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

func (h *handler) noticePage(c *gin.Context, who caller) {
	page, err := h.svc.Tenants.NoticePage(c.Request.Context(), who.tenantID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("title"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) noticeGet(c *gin.Context, who caller) {
	item, err := h.svc.Tenants.NoticeByID(c.Request.Context(), who.tenantID, int64(atoi(c.Query("id"), 0)))
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) noticeSave(c *gin.Context, who caller) {
	var item Notice
	if err := c.ShouldBindJSON(&item); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveNotice(c.Request.Context(), who.tenantID, item)
	writeIDOrBool(c, c.Request.Method == http.MethodPost, id, err)
}

func (h *handler) noticeDelete(c *gin.Context, who caller) {
	if err := h.svc.Tenants.DeleteNotice(c.Request.Context(), who.tenantID, int64(atoi(c.Query("id"), 0))); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) noticeDeleteList(c *gin.Context, who caller) {
	for _, id := range queryIDList(c) {
		if err := h.svc.Tenants.DeleteNotice(c.Request.Context(), who.tenantID, id); err != nil {
			writeErr(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

// noticePush 确认公告存在后，推给在线的管理员。
func (h *handler) noticePush(c *gin.Context, who caller) {
	item, err := h.svc.Tenants.NoticeByID(c.Request.Context(), who.tenantID, int64(atoi(c.Query("id"), 0)))
	if err != nil {
		writeErr(c, err)
		return
	}
	if item == nil {
		writeErr(c, &Error{Code: 1_002_008_001, Msg: "当前通知公告不存在"})
		return
	}
	if h.svc.OnNotice != nil {
		h.svc.OnNotice(who.tenantID, *item)
	}
	httpx.OK(c, true)
}

func (h *handler) profileGet(c *gin.Context, who caller) {
	user, err := h.svc.UserGet(c.Request.Context(), who.tenantID, who.userID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if user == nil {
		writeErr(c, &Error{Code: codeUserNotExists, Msg: "用户不存在"})
		return
	}
	roleIDs, err := h.svc.Access.UserRoleIDs(c.Request.Context(), who.tenantID, who.userID)
	if err != nil {
		writeErr(c, err)
		return
	}
	roles := make([]gin.H, 0, len(roleIDs))
	for _, id := range roleIDs {
		role, err := h.svc.Access.RoleByID(c.Request.Context(), who.tenantID, id)
		if err != nil {
			writeErr(c, err)
			return
		}
		if role != nil {
			roles = append(roles, gin.H{"id": role.ID, "name": role.Name})
		}
	}
	var dept any
	if user.DeptID != nil {
		dept = gin.H{"id": *user.DeptID, "name": user.DeptName}
	}
	httpx.OK(c, gin.H{
		"id": user.ID, "username": user.Username, "nickname": user.Nickname,
		"email": user.Email, "mobile": user.Mobile, "sex": user.Sex, "avatar": user.Avatar,
		"loginIp": user.LoginIP, "loginDate": user.LoginDate, "createTime": user.CreateTime,
		"roles": roles, "dept": dept, "posts": []any{},
	})
}

func (h *handler) profileUpdate(c *gin.Context, who caller) {
	var req struct {
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
		Mobile   string `json:"mobile"`
		Sex      *int   `json:"sex"`
		Avatar   string `json:"avatar"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.Tenants.UpdateProfile(c.Request.Context(), who.userID, req.Nickname, req.Email, req.Mobile, req.Avatar, req.Sex); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) profilePassword(c *gin.Context, who caller) {
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := h.svc.ChangeOwnPassword(c.Request.Context(), who.userID, req.OldPassword, req.NewPassword); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func queryStatus(c *gin.Context) *int {
	text := c.Query("status")
	if text == "" {
		return nil
	}
	value := atoi(text, 0)
	return &value
}

func queryIDList(c *gin.Context) []int64 {
	var ids []int64
	for _, text := range c.QueryArray("ids") {
		for _, part := range splitComma(text) {
			if part == "" {
				continue
			}
			ids = append(ids, int64(atoi(part, 0)))
		}
	}
	return ids
}

func splitComma(text string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == ',' {
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	return append(parts, text[start:])
}

func writePage[T any](c *gin.Context, page Page[T], err error) {
	if err != nil {
		writeErr(c, err)
		return
	}
	if page.List == nil {
		page.List = []T{}
	}
	httpx.OK(c, page)
}

func writeList[T any](c *gin.Context, list []T, err error) {
	if err != nil {
		writeErr(c, err)
		return
	}
	if list == nil {
		list = []T{}
	}
	httpx.OK(c, list)
}

func writeErr(c *gin.Context, err error) {
	if biz, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
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
