package permissionrpc

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/directory"
)

// MountRPC 挂载 Java PermissionApi/PermissionCommonApi 的四个 Feign 路由。
// 与 mini 一样依赖 tenant-id 传播；部署时仍须把 /rpc-api 隔离在可信内部网络。
func MountRPC(r *gin.Engine, svc *Service, validateTenant func(context.Context, int64, time.Time) error) {
	h := rpcHandler{svc: svc, validateTenant: validateTenant}
	rpc := r.Group("/rpc-api/system/permission")
	rpc.GET("/user-role-id-list-by-role-id", h.userIDsByRoles)
	rpc.GET("/has-any-permissions", h.hasAnyPermissions)
	rpc.GET("/has-any-roles", h.hasAnyRoles)
	rpc.GET("/get-dept-data-permission", h.deptDataPermission)
}

type rpcHandler struct {
	svc            *Service
	validateTenant func(context.Context, int64, time.Time) error
}

func (h rpcHandler) tenant(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.GetHeader("tenant-id"), 10, 64)
	if err != nil || id <= 0 {
		h.fail(c, 400, "请求的租户标识未传递，请进行排查")
		return 0, false
	}
	if h.validateTenant == nil {
		h.fail(c, 500, "系统异常")
		return 0, false
	}
	if err := h.validateTenant(c.Request.Context(), id, time.Now()); err != nil {
		h.failError(c, err)
		return 0, false
	}
	return id, true
}

func (h rpcHandler) userIDsByRoles(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	values, ok := h.values(c, "roleIds")
	if !ok {
		return
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			h.fail(c, 400, "请求参数不正确")
			return
		}
		ids = append(ids, id)
	}
	userIDs, err := h.svc.UserIDsByRoleIDs(c.Request.Context(), tenantID, ids)
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, userIDs)
}

func (h rpcHandler) hasAnyPermissions(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	permissions, ok := h.values(c, "permissions")
	if !ok {
		return
	}
	result, err := h.svc.HasAnyPermissions(c.Request.Context(), tenantID, userID, permissions)
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, result)
}

func (h rpcHandler) hasAnyRoles(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	roles, ok := h.values(c, "roles")
	if !ok {
		return
	}
	result, err := h.svc.HasAnyRoles(c.Request.Context(), tenantID, userID, roles)
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, result)
}

func (h rpcHandler) deptDataPermission(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userID, ok := h.userID(c)
	if !ok {
		return
	}
	result, err := h.svc.GetDeptDataPermission(c.Request.Context(), tenantID, userID)
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, result)
}

func (h rpcHandler) userID(c *gin.Context) (int64, bool) {
	value, ok := c.GetQuery("userId")
	if !ok {
		h.fail(c, 400, "请求参数不正确")
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		h.fail(c, 400, "请求参数不正确")
		return 0, false
	}
	return id, true
}

// Spring 的数组参数允许重复键或逗号分隔；空字符串按空数组处理。
func (h rpcHandler) values(c *gin.Context, key string) ([]string, bool) {
	values, present := c.Request.URL.Query()[key]
	if !present {
		h.fail(c, 400, "请求参数不正确")
		return nil, false
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result = append(result, part)
			if len(result) > 1000 {
				h.fail(c, 400, "请求参数过多")
				return nil, false
			}
		}
	}
	return result, true
}

func (h rpcHandler) failError(c *gin.Context, err error) {
	var biz *directory.Error
	if errors.As(err, &biz) {
		h.fail(c, biz.Code, biz.Msg)
		return
	}
	h.fail(c, 500, "系统异常")
}

func (rpcHandler) fail(c *gin.Context, code int, msg string) {
	httpx.Fail(c, http.StatusOK, code, msg)
}
