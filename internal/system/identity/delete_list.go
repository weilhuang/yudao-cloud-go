package identity

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func (h *handler) clientDeleteList(c *gin.Context, _ caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.db.DeleteClientList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	if err := h.svc.evictCommitted(c.Request.Context(), "oauth_client"); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) socialDeleteList(c *gin.Context, who caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.db.DeleteSocialList(c.Request.Context(), who.tenantID, ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func requiredIDs(c *gin.Context) ([]int64, bool) {
	values, present := c.Request.URL.Query()["ids"]
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
		}
	}
	return ids, true
}
