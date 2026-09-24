package directory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// RPCUser 与 Java AdminUserRespDTO 保持相同字段；PostIDs 的 nil 表示数据库中的 null。
type RPCUser struct {
	ID       int64   `json:"id"`
	Nickname string  `json:"nickname"`
	Status   int     `json:"status"`
	DeptID   *int64  `json:"deptId"`
	PostIDs  []int64 `json:"postIds"`
	Mobile   string  `json:"mobile"`
	Email    string  `json:"email"`
	Sex      *int    `json:"sex"`
	Avatar   string  `json:"avatar"`
}

// RPCDept 与 Java DeptRespDTO 保持相同字段。
type RPCDept struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	ParentID     int64  `json:"parentId"`
	LeaderUserID *int64 `json:"leaderUserId"`
	Status       int    `json:"status"`
}

// RPCUserFilter 的每次调用只设置一种条件，避免将外部参数拼接成 SQL。
type RPCUserFilter struct {
	IDs      []int64
	DeptIDs  []int64
	PostIDs  []int64
	Mobile   *string
	Nickname *string
	// ApplyAccess 为真时套用上下文里的部门数据范围。按编号和手机号查询在 Java 里显式关闭。
	ApplyAccess bool
}

// RPCReader 是 Java AdminUserApi、DeptApi 和 DictDataApi 的只读数据库边界。
type RPCReader interface {
	RPCUsers(ctx context.Context, tenantID int64, filter RPCUserFilter) ([]RPCUser, error)
	RPCDepts(ctx context.Context, tenantID int64, ids []int64) ([]RPCDept, error)
	RPCChildDepts(ctx context.Context, tenantID int64, parentIDs []int64) ([]RPCDept, error)
	RPCDictDataList(ctx context.Context, dictType string) ([]RPCDictData, error)
	RPCDictDataValues(ctx context.Context, dictType string, values []string) ([]RPCDictData, error)
}

// MountRPC 挂载 Java 用户、部门和字典 Feign 接口，并对每次请求校验租户状态。
// scope 在请求带管理员 login-user 时计算数据范围。按编号查用户仍不套范围。
func MountRPC(r *gin.Engine, reader RPCReader, validateTenant func(context.Context, int64, time.Time) error, scope func(context.Context, int64, int64) (UserAccess, error)) {
	h := rpcHandler{reader: reader, validateTenant: validateTenant, scope: scope}
	users := r.Group("/rpc-api/system/user")
	users.GET("/get", h.userGet)
	users.GET("/get-by-mobile", h.userByMobile)
	users.GET("/list-by-subordinate", h.userSubordinates)
	users.GET("/list", h.userList)
	users.GET("/list-by-dept-id", h.userByDeptIDs)
	users.GET("/list-by-post-id", h.userByPostIDs)
	users.GET("/list-by-nickname", h.userByNickname)
	users.GET("/valid", h.userValid)

	depts := r.Group("/rpc-api/system/dept")
	depts.GET("/get", h.deptGet)
	depts.GET("/list", h.deptList)
	depts.GET("/valid", h.deptValid)
	depts.GET("/list-child", h.deptChildren)
	depts.GET("/list-child-by-ids", h.deptChildrenByIDs)
	depts.GET("/list-parent", h.deptParents)

	dicts := r.Group("/rpc-api/system/dict-data")
	dicts.GET("/valid", h.dictValid)
	dicts.GET("/list", h.dictList)
}

type rpcHandler struct {
	reader         RPCReader
	validateTenant func(context.Context, int64, time.Time) error
	scope          func(context.Context, int64, int64) (UserAccess, error)
}

func (h rpcHandler) tenant(c *gin.Context) (int64, bool) {
	tenantID, ok := headerTenant(c)
	if !ok || tenantID <= 0 {
		httpx.Fail(c, http.StatusOK, 400, "请求的租户标识未传递，请进行排查")
		return 0, false
	}
	if h.validateTenant == nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return 0, false
	}
	if err := h.validateTenant(c.Request.Context(), tenantID, time.Now()); err != nil {
		writeErr(c, err)
		return 0, false
	}
	return tenantID, true
}

// Spring 的 Collection<Long> 接受 ids=1,2 和重复参数；两个形式同时支持。
func rpcIDs(c *gin.Context, key string) ([]int64, bool) {
	values, present := c.Request.URL.Query()[key]
	if !present {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return nil, false
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
				httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
				return nil, false
			}
			ids = append(ids, id)
			if len(ids) > 1000 {
				httpx.Fail(c, http.StatusOK, 400, "请求参数过多")
				return nil, false
			}
		}
	}
	return ids, true
}

func rpcID(c *gin.Context) (int64, bool) {
	ids, ok := rpcIDs(c, "id")
	if !ok {
		return 0, false
	}
	if len(ids) != 1 {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return 0, false
	}
	return ids[0], true
}

func rpcText(c *gin.Context, key string) (string, bool) {
	value, ok := c.GetQuery(key)
	if !ok {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
	}
	return value, ok
}

func rpcOne[T any](c *gin.Context, list []T, err error) {
	if err != nil {
		writeErr(c, err)
		return
	}
	if len(list) == 0 {
		httpx.OK(c, nil)
		return
	}
	httpx.OK(c, list[0])
}

func (h rpcHandler) userGet(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	id, ok := rpcID(c)
	if !ok {
		return
	}
	list, err := h.reader.RPCUsers(c.Request.Context(), tenantID, RPCUserFilter{IDs: []int64{id}})
	rpcOne(c, list, err)
}

func (h rpcHandler) userByMobile(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	mobile, ok := rpcText(c, "mobile")
	if !ok {
		return
	}
	list, err := h.reader.RPCUsers(c.Request.Context(), tenantID, RPCUserFilter{Mobile: &mobile})
	if err == nil && len(list) > 1 {
		// Java 的 selectOne 遇到同租户重复手机号会失败，不能任取其中一个。
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return
	}
	rpcOne(c, list, err)
}

func (h rpcHandler) userList(c *gin.Context) {
	h.usersByIDs(c, "ids", false, func(ids []int64) RPCUserFilter { return RPCUserFilter{IDs: ids} })
}
func (h rpcHandler) userByDeptIDs(c *gin.Context) {
	h.usersByIDs(c, "deptIds", true, func(ids []int64) RPCUserFilter { return RPCUserFilter{DeptIDs: ids, ApplyAccess: true} })
}
func (h rpcHandler) userByPostIDs(c *gin.Context) {
	h.usersByIDs(c, "postIds", true, func(ids []int64) RPCUserFilter { return RPCUserFilter{PostIDs: ids, ApplyAccess: true} })
}

func (h rpcHandler) usersByIDs(c *gin.Context, key string, scoped bool, filter func([]int64) RPCUserFilter) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if scoped {
		ctx, ok = h.accessCtx(c, tenantID)
		if !ok {
			return
		}
	}
	ids, ok := rpcIDs(c, key)
	if !ok {
		return
	}
	if len(ids) == 0 {
		httpx.OK(c, []RPCUser{})
		return
	}
	list, err := h.reader.RPCUsers(ctx, tenantID, scopedFilter(ctx, filter(ids)))
	writeList(c, list, err)
}

func (h rpcHandler) userByNickname(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	nickname, ok := rpcText(c, "nickname")
	if !ok {
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list, err := h.reader.RPCUsers(ctx, tenantID, scopedFilter(ctx, RPCUserFilter{Nickname: &nickname, ApplyAccess: true}))
	writeList(c, list, err)
}

func (h rpcHandler) userValid(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if len(ids) == 0 {
		httpx.OK(c, true)
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list, err := h.reader.RPCUsers(ctx, tenantID, scopedFilter(ctx, RPCUserFilter{IDs: ids, ApplyAccess: true}))
	if err != nil {
		writeErr(c, err)
		return
	}
	byID := make(map[int64]RPCUser, len(list))
	for _, user := range list {
		byID[user.ID] = user
	}
	for _, id := range ids {
		user, exists := byID[id]
		if !exists {
			writeErr(c, &Error{Code: 1_002_003_003, Msg: "用户不存在"})
			return
		}
		if user.Status != 0 {
			writeErr(c, &Error{Code: 1_002_003_006, Msg: "名字为【" + user.Nickname + "】的用户已被禁用"})
			return
		}
	}
	httpx.OK(c, true)
}

func (h rpcHandler) userSubordinates(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	id, ok := rpcID(c)
	if !ok {
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	users, err := h.reader.RPCUsers(ctx, tenantID, scopedFilter(ctx, RPCUserFilter{IDs: []int64{id}, ApplyAccess: true}))
	if err != nil {
		writeErr(c, err)
		return
	}
	if len(users) == 0 || users[0].DeptID == nil {
		httpx.OK(c, []RPCUser{})
		return
	}
	depts, err := h.reader.RPCDepts(ctx, tenantID, []int64{*users[0].DeptID})
	if err != nil {
		writeErr(c, err)
		return
	}
	if len(depts) == 0 || depts[0].LeaderUserID == nil || *depts[0].LeaderUserID != id {
		httpx.OK(c, []RPCUser{})
		return
	}
	children, err := h.childDepts(ctx, tenantID, []int64{depts[0].ID})
	if err != nil {
		writeErr(c, err)
		return
	}
	deptIDs := []int64{depts[0].ID}
	for _, child := range children {
		deptIDs = append(deptIDs, child.ID)
	}
	list, err := h.reader.RPCUsers(ctx, tenantID, scopedFilter(ctx, RPCUserFilter{DeptIDs: deptIDs, ApplyAccess: true}))
	if err != nil {
		writeErr(c, err)
		return
	}
	result := make([]RPCUser, 0, len(list))
	for _, user := range list {
		if user.ID != id {
			result = append(result, user)
		}
	}
	httpx.OK(c, result)
}

func (h rpcHandler) deptGet(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	id, ok := rpcID(c)
	if !ok {
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list, err := h.reader.RPCDepts(ctx, tenantID, []int64{id})
	rpcOne(c, list, err)
}

func (h rpcHandler) deptList(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if len(ids) == 0 {
		httpx.OK(c, []RPCDept{})
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list, err := h.reader.RPCDepts(ctx, tenantID, ids)
	writeList(c, list, err)
}

func (h rpcHandler) deptValid(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if len(ids) == 0 {
		httpx.OK(c, true)
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list, err := h.reader.RPCDepts(ctx, tenantID, ids)
	if err != nil {
		writeErr(c, err)
		return
	}
	byID := make(map[int64]RPCDept, len(list))
	for _, dept := range list {
		byID[dept.ID] = dept
	}
	for _, id := range ids {
		dept, exists := byID[id]
		if !exists {
			writeErr(c, &Error{Code: 1_002_004_002, Msg: "当前部门不存在"})
			return
		}
		if dept.Status != 0 {
			writeErr(c, &Error{Code: 1_002_004_006, Msg: "部门(" + dept.Name + ")不处于开启状态，不允许选择"})
			return
		}
	}
	httpx.OK(c, true)
}

func (h rpcHandler) deptChildren(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	id, ok := rpcID(c)
	if !ok {
		return
	}
	h.writeChildren(c, tenantID, []int64{id})
}

func (h rpcHandler) deptChildrenByIDs(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	h.writeChildren(c, tenantID, ids)
}

func (h rpcHandler) writeChildren(c *gin.Context, tenantID int64, ids []int64) {
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list, err := h.childDepts(ctx, tenantID, ids)
	writeList(c, list, err)
}

// childDepts 按层查询；已访问集合让脏数据环路不能让 RPC 无限循环。
func (h rpcHandler) childDepts(ctx context.Context, tenantID int64, parentIDs []int64) ([]RPCDept, error) {
	result := make([]RPCDept, 0)
	visited := make(map[int64]bool, len(parentIDs))
	for _, id := range parentIDs {
		visited[id] = true
	}
	for len(parentIDs) > 0 {
		children, err := h.reader.RPCChildDepts(ctx, tenantID, parentIDs)
		if err != nil {
			return nil, err
		}
		parentIDs = nil
		for _, child := range children {
			if visited[child.ID] {
				continue
			}
			visited[child.ID] = true
			result = append(result, child)
			parentIDs = append(parentIDs, child.ID)
		}
	}
	return result, nil
}

func (h rpcHandler) deptParents(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	id, ok := rpcID(c)
	if !ok {
		return
	}
	ctx, ok := h.accessCtx(c, tenantID)
	if !ok {
		return
	}
	list := make([]RPCDept, 0)
	visited := map[int64]bool{id: true}
	depts, err := h.reader.RPCDepts(ctx, tenantID, []int64{id})
	if err != nil {
		writeErr(c, err)
		return
	}
	for len(depts) > 0 && depts[0].ParentID != 0 && !visited[depts[0].ParentID] {
		parentID := depts[0].ParentID
		visited[parentID] = true
		parent, err := h.reader.RPCDepts(ctx, tenantID, []int64{parentID})
		if err != nil {
			writeErr(c, err)
			return
		}
		if len(parent) == 0 {
			break
		}
		list = append(list, parent[0])
		depts = parent
	}
	httpx.OK(c, list)
}

// accessCtx 在请求带管理员 login-user 时写入数据范围。没有这个头，或用户不是管理员时不追加条件。
func (h rpcHandler) accessCtx(c *gin.Context, tenantID int64) (context.Context, bool) {
	raw := c.GetHeader("login-user")
	if raw == "" {
		return c.Request.Context(), true
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return nil, false
	}
	var user struct {
		ID       int64 `json:"id"`
		UserType int   `json:"userType"`
	}
	if json.Unmarshal([]byte(decoded), &user) != nil || user.ID <= 0 {
		httpx.Fail(c, http.StatusOK, 400, "请求参数不正确")
		return nil, false
	}
	if user.UserType != 2 {
		return c.Request.Context(), true
	}
	if h.scope == nil {
		httpx.Fail(c, http.StatusOK, 500, "系统异常")
		return nil, false
	}
	access, err := h.scope(c.Request.Context(), tenantID, user.ID)
	if err != nil {
		writeErr(c, err)
		return nil, false
	}
	access.UserID = user.ID
	return withUserAccess(c.Request.Context(), access), true
}

func scopedFilter(ctx context.Context, filter RPCUserFilter) RPCUserFilter {
	if accessFrom(ctx) == nil {
		filter.ApplyAccess = false
	}
	return filter
}
