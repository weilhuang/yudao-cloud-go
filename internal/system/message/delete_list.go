package message

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func (h *handler) notifyDeleteList(c *gin.Context, _ caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.db.DeleteNotifyList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	if err := h.svc.evictCommitted(c.Request.Context(), "notify_template"); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) accountDeleteList(c *gin.Context, _ caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteAccountList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) mailDeleteList(c *gin.Context, _ caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.db.DeleteMailList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	if err := h.svc.evictCommitted(c.Request.Context(), "mail_template"); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) channelDeleteList(c *gin.Context, _ caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteChannelList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) smsDeleteList(c *gin.Context, _ caller) {
	ids, ok := requiredIDs(c)
	if !ok {
		return
	}
	if err := h.db.DeleteSmsList(c.Request.Context(), ids); err != nil {
		writeBiz(c, err)
		return
	}
	if err := h.svc.evictCommitted(c.Request.Context(), "sms_template"); err != nil {
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
