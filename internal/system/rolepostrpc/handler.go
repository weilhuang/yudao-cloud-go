package rolepostrpc

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

// MountRPC 挂载 Java PostApi 与 RoleApi 的五个 Feign 路由。
// 与 mini 一样依赖 tenant-id 传播；/rpc-api 必须由部署网络隔离。
func MountRPC(r *gin.Engine, svc *Service, validateTenant func(context.Context, int64, time.Time) error) {
	h := rpcHandler{svc: svc, validateTenant: validateTenant}
	posts := r.Group("/rpc-api/system/post")
	posts.GET("/valid", h.postValid)
	posts.GET("/list", h.postList)
	roles := r.Group("/rpc-api/system/role")
	roles.GET("/valid", h.roleValid)
	roles.GET("/get", h.roleGet)
	roles.GET("/list", h.roleList)
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

// ids 同时支持 Feign 的逗号分隔参数和重复 ids 参数。空值对应 Java 空 Collection。
func (h rpcHandler) ids(c *gin.Context, key string) ([]int64, bool) {
	values, exists := c.Request.URL.Query()[key]
	if !exists {
		h.fail(c, 400, "请求参数不正确")
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
				h.fail(c, 400, "请求参数不正确")
				return nil, false
			}
			ids = append(ids, id)
			if len(ids) > 1000 {
				h.fail(c, 400, "请求参数过多")
				return nil, false
			}
		}
	}
	return ids, true
}

func (h rpcHandler) postValid(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := h.ids(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.ValidPostList(c.Request.Context(), tenantID, ids); err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h rpcHandler) postList(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := h.ids(c, "ids")
	if !ok {
		return
	}
	posts, err := h.svc.PostList(c.Request.Context(), tenantID, ids)
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, posts)
}

func (h rpcHandler) roleValid(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := h.ids(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.ValidRoleList(c.Request.Context(), tenantID, ids); err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h rpcHandler) roleGet(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := h.ids(c, "id")
	if !ok {
		return
	}
	if len(ids) != 1 {
		h.fail(c, 400, "请求参数不正确")
		return
	}
	role, err := h.svc.RoleGet(c.Request.Context(), tenantID, ids[0])
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, role)
}

func (h rpcHandler) roleList(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	ids, ok := h.ids(c, "ids")
	if !ok {
		return
	}
	roles, err := h.svc.RoleList(c.Request.Context(), tenantID, ids)
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, roles)
}

func (h rpcHandler) failError(c *gin.Context, err error) {
	var biz *Error
	if errors.As(err, &biz) {
		h.fail(c, biz.Code, biz.Msg)
		return
	}
	var tenantErr *directory.Error
	if errors.As(err, &tenantErr) {
		h.fail(c, tenantErr.Code, tenantErr.Msg)
		return
	}
	h.fail(c, 500, "系统异常")
}

func (rpcHandler) fail(c *gin.Context, code int, msg string) {
	httpx.Fail(c, http.StatusOK, code, msg)
}
