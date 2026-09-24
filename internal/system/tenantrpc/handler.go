package tenantrpc

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// MountRPC 挂载 TenantCommonApi。两个方法都有 @TenantIgnore，不能要求 tenant-id。
// /rpc-api 仍应由部署网络限制为内部访问，避免泄露全部租户编号。
func MountRPC(r *gin.Engine, svc *Service) {
	h := rpcHandler{svc: svc}
	rpc := r.Group("/rpc-api/system/tenant")
	rpc.GET("/id-list", h.idList)
	rpc.GET("/valid", h.valid)
}

type rpcHandler struct{ svc *Service }

func (h rpcHandler) idList(c *gin.Context) {
	ids, err := h.svc.TenantIDList(c.Request.Context())
	if err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, ids)
}

func (h rpcHandler) valid(c *gin.Context) {
	raw, present := c.GetQuery("id")
	if !present || raw == "" {
		httpx.Fail(c, http.StatusOK, http.StatusBadRequest, "请求参数缺失:id")
		return
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		httpx.Fail(c, http.StatusOK, http.StatusBadRequest, "请求参数类型错误:id")
		return
	}
	if err := h.svc.ValidTenant(c.Request.Context(), id, time.Now()); err != nil {
		h.failError(c, err)
		return
	}
	httpx.OK(c, true)
}

func (rpcHandler) failError(c *gin.Context, err error) {
	var business *Error
	if errors.As(err, &business) {
		httpx.Fail(c, http.StatusOK, business.Code, business.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, http.StatusInternalServerError, "系统异常")
}
