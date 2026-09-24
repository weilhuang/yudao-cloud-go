package message

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// Mount 挂上站内信、邮件和短信。未读数量给右上角轮询，只要求登录。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service, db *MySQL) {
	h := &handler{sessions: sessions, svc: svc, db: db}
	notify := r.Group("/admin-api/system/notify-template")
	notify.POST("/create", h.permit("system:notify-template:create", h.notifySave))
	notify.PUT("/update", h.permit("system:notify-template:update", h.notifySave))
	notify.DELETE("/delete", h.permit("system:notify-template:delete", h.notifyDelete))
	notify.DELETE("/delete-list", h.permit("system:notify-template:delete", h.notifyDeleteList))
	notify.GET("/get", h.permit("system:notify-template:query", h.notifyGet))
	notify.GET("/page", h.permit("system:notify-template:query", h.notifyPage))
	notify.GET("/simple-list", h.login(h.notifySimple))
	notify.GET("/list-all-simple", h.login(h.notifySimple))
	notify.POST("/send-notify", h.permit("system:notify-template:send-notify", h.notifySend))

	msg := r.Group("/admin-api/system/notify-message")
	msg.GET("/get", h.permit("system:notify-message:query", h.messageGet))
	msg.GET("/page", h.permit("system:notify-message:query", h.messagePage))
	msg.GET("/my-page", h.login(h.myPage))
	msg.PUT("/update-read", h.login(h.markRead))
	msg.PUT("/update-all-read", h.login(h.markAll))
	msg.GET("/get-unread-list", h.login(h.unread))
	msg.GET("/get-unread-count", h.login(h.unreadCount))

	account := r.Group("/admin-api/system/mail-account")
	account.POST("/create", h.permit("system:mail-account:create", h.accountSave))
	account.PUT("/update", h.permit("system:mail-account:update", h.accountSave))
	account.DELETE("/delete", h.permit("system:mail-account:delete", h.accountDelete))
	account.DELETE("/delete-list", h.permit("system:mail-account:delete", h.accountDeleteList))
	account.GET("/get", h.permit("system:mail-account:query", h.accountGet))
	account.GET("/page", h.permit("system:mail-account:query", h.accountPage))
	account.GET("/simple-list", h.login(h.accountSimple))
	account.GET("/list-all-simple", h.login(h.accountSimple))

	mail := r.Group("/admin-api/system/mail-template")
	mail.POST("/create", h.permit("system:mail-template:create", h.mailSave))
	mail.PUT("/update", h.permit("system:mail-template:update", h.mailSave))
	mail.DELETE("/delete", h.permit("system:mail-template:delete", h.mailDelete))
	mail.DELETE("/delete-list", h.permit("system:mail-template:delete", h.mailDeleteList))
	mail.GET("/get", h.permit("system:mail-template:query", h.mailGet))
	mail.GET("/page", h.permit("system:mail-template:query", h.mailPage))
	mail.GET("/simple-list", h.login(h.mailSimple))
	mail.GET("/list-all-simple", h.login(h.mailSimple))
	mail.POST("/send-mail", h.permit("system:mail-template:send-mail", h.mailSend))

	mailLog := r.Group("/admin-api/system/mail-log")
	mailLog.GET("/page", h.permit("system:mail-log:query", h.mailLogPage))
	mailLog.GET("/get", h.permit("system:mail-log:query", h.mailLogGet))

	channel := r.Group("/admin-api/system/sms-channel")
	channel.POST("/create", h.permit("system:sms-channel:create", h.channelSave))
	channel.PUT("/update", h.permit("system:sms-channel:update", h.channelSave))
	channel.DELETE("/delete", h.permit("system:sms-channel:delete", h.channelDelete))
	channel.DELETE("/delete-list", h.permit("system:sms-channel:delete", h.channelDeleteList))
	channel.GET("/get", h.permit("system:sms-channel:query", h.channelGet))
	channel.GET("/page", h.permit("system:sms-channel:query", h.channelPage))
	channel.GET("/simple-list", h.login(h.channelSimple))
	channel.GET("/list-all-simple", h.login(h.channelSimple))

	sms := r.Group("/admin-api/system/sms-template")
	sms.POST("/create", h.permit("system:sms-template:create", h.smsSave))
	sms.PUT("/update", h.permit("system:sms-template:update", h.smsSave))
	sms.DELETE("/delete", h.permit("system:sms-template:delete", h.smsDelete))
	sms.DELETE("/delete-list", h.permit("system:sms-template:delete", h.smsDeleteList))
	sms.GET("/get", h.permit("system:sms-template:query", h.smsGet))
	sms.GET("/page", h.permit("system:sms-template:query", h.smsPage))
	sms.GET("/export-excel", h.permit("system:sms-template:export", h.smsExport))
	sms.GET("/simple-list", h.login(h.smsSimple))
	sms.GET("/list-all-simple", h.login(h.smsSimple))
	sms.POST("/send-sms", h.permit("system:sms-template:send-sms", h.smsSend))

	smsLog := r.Group("/admin-api/system/sms-log")
	smsLog.GET("/page", h.permit("system:sms-log:query", h.smsLogPage))
	smsLog.GET("/export-excel", h.permit("system:sms-log:export", h.smsLogExport))
	smsLog.GET("/get", h.permit("system:sms-log:query", h.smsLogGet))

	callback := r.Group("/admin-api/system/sms/callback")
	callback.POST("/aliyun", h.smsCallback("ALIYUN"))
	callback.POST("/tencent", h.smsCallback("TENCENT"))
	callback.POST("/huawei", h.smsCallback("HUAWEI"))
	callback.POST("/qiniu", h.smsCallback("QINIU"))
}

type handler struct {
	sessions *auth.Service
	svc      *Service
	db       *MySQL
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
		next(c, caller{userID: userID, tenantID: tenantID})
	}
}

func (h *handler) notifySave(c *gin.Context, _ caller) {
	var item NotifyTemplate
	if !bind(c, &item) {
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveNotify(c.Request.Context(), item)
	writeID(c, id, err)
}

func (h *handler) notifyDelete(c *gin.Context, _ caller) {
	if err := h.db.DeleteNotify(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) notifyGet(c *gin.Context, _ caller) {
	item, err := h.db.NotifyByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) notifyPage(c *gin.Context, _ caller) {
	page, err := h.db.NotifyPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), c.Query("code"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) notifySimple(c *gin.Context, _ caller) {
	list, err := h.db.NotifySimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) notifySend(c *gin.Context, who caller) {
	var req struct {
		UserID         int64          `json:"userId"`
		UserType       int            `json:"userType"`
		TemplateCode   string         `json:"templateCode"`
		TemplateParams map[string]any `json:"templateParams"`
	}
	if !bind(c, &req) {
		return
	}
	id, err := h.svc.SendNotify(c.Request.Context(), who.tenantID, req.UserID, req.UserType, req.TemplateCode, req.TemplateParams)
	writeID(c, id, err)
}

func (h *handler) messageGet(c *gin.Context, _ caller) {
	item, err := h.db.MessageByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) messagePage(c *gin.Context, who caller) {
	page, err := h.db.MessagePage(c.Request.Context(), who.tenantID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), queryInt(c, "userId"))
	writePage(c, page, err)
}

func (h *handler) myPage(c *gin.Context, who caller) {
	page, err := h.db.MyPage(c.Request.Context(), who.tenantID, who.userID, atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), queryBool(c, "readStatus"))
	writePage(c, page, err)
}

func (h *handler) markRead(c *gin.Context, who caller) {
	if err := h.db.MarkRead(c.Request.Context(), who.tenantID, who.userID, queryIDs(c)); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) markAll(c *gin.Context, who caller) {
	if err := h.db.MarkAllRead(c.Request.Context(), who.tenantID, who.userID); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) unread(c *gin.Context, who caller) {
	list, err := h.db.Unread(c.Request.Context(), who.tenantID, who.userID, atoi(c.Query("size"), 10))
	writeList(c, list, err)
}

func (h *handler) unreadCount(c *gin.Context, who caller) {
	n, err := h.db.UnreadCount(c.Request.Context(), who.tenantID, who.userID)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, n)
}

func (h *handler) accountSave(c *gin.Context, _ caller) {
	var item MailAccount
	if !bind(c, &item) {
		return
	}
	var id int64
	var err error
	if c.Request.Method == http.MethodPost {
		item.ID = 0
		id, err = h.db.CreateAccount(c.Request.Context(), item)
	} else {
		err = h.db.UpdateAccount(c.Request.Context(), item)
		id = item.ID
	}
	writeID(c, id, err)
}

func (h *handler) accountDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteAccount(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) accountGet(c *gin.Context, _ caller) {
	item, err := h.db.AccountByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) accountPage(c *gin.Context, _ caller) {
	page, err := h.db.AccountPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("mail"))
	writePage(c, page, err)
}

func (h *handler) accountSimple(c *gin.Context, _ caller) {
	list, err := h.db.AccountSimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) mailSave(c *gin.Context, _ caller) {
	var item MailTemplate
	if !bind(c, &item) {
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveMail(c.Request.Context(), item)
	writeID(c, id, err)
}

func (h *handler) mailDelete(c *gin.Context, _ caller) {
	if err := h.db.DeleteMail(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) mailGet(c *gin.Context, _ caller) {
	item, err := h.db.MailByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) mailPage(c *gin.Context, _ caller) {
	page, err := h.db.MailPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), c.Query("code"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) mailSimple(c *gin.Context, _ caller) {
	list, err := h.db.MailSimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) mailSend(c *gin.Context, _ caller) {
	var req struct {
		ToMails        []string       `json:"toMails"`
		TemplateCode   string         `json:"templateCode"`
		TemplateParams map[string]any `json:"templateParams"`
	}
	if !bind(c, &req) {
		return
	}
	id, err := h.svc.SendMail(c.Request.Context(), req.ToMails, req.TemplateCode, req.TemplateParams)
	writeID(c, id, err)
}

func (h *handler) mailLogPage(c *gin.Context, _ caller) {
	page, err := h.db.MailLogPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("toMail"), querySendStatus(c))
	writePage(c, page, err)
}

func (h *handler) mailLogGet(c *gin.Context, _ caller) {
	item, err := h.db.MailLogByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, &item, err)
}

func (h *handler) channelSave(c *gin.Context, _ caller) {
	var item SmsChannel
	if !bind(c, &item) {
		return
	}
	var id int64
	var err error
	if c.Request.Method == http.MethodPost {
		id, err = h.db.CreateChannel(c.Request.Context(), item)
	} else {
		err = h.db.UpdateChannel(c.Request.Context(), item)
		id = item.ID
	}
	writeID(c, id, err)
}

func (h *handler) channelDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteChannel(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) channelGet(c *gin.Context, _ caller) {
	item, err := h.db.ChannelByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) channelPage(c *gin.Context, _ caller) {
	page, err := h.db.ChannelPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("signature"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) channelSimple(c *gin.Context, _ caller) {
	list, err := h.db.ChannelSimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) smsSave(c *gin.Context, _ caller) {
	var item SmsTemplate
	if !bind(c, &item) {
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveSms(c.Request.Context(), item)
	writeID(c, id, err)
}

func (h *handler) smsDelete(c *gin.Context, _ caller) {
	if err := h.db.DeleteSms(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) smsGet(c *gin.Context, _ caller) {
	item, err := h.db.SmsByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) smsPage(c *gin.Context, _ caller) {
	page, err := h.db.SmsPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("code"), queryInt(c, "channelId"), queryStatus(c))
	writePage(c, page, err)
}

func (h *handler) smsSimple(c *gin.Context, _ caller) {
	list, err := h.db.SmsSimple(c.Request.Context())
	writeList(c, list, err)
}

func (h *handler) smsSend(c *gin.Context, _ caller) {
	var req struct {
		Mobile         string         `json:"mobile"`
		TemplateCode   string         `json:"templateCode"`
		TemplateParams map[string]any `json:"templateParams"`
	}
	if !bind(c, &req) {
		return
	}
	id, err := h.svc.SendSms(c.Request.Context(), req.Mobile, req.TemplateCode, req.TemplateParams)
	writeID(c, id, err)
}

func (h *handler) smsLogPage(c *gin.Context, _ caller) {
	page, err := h.db.SmsLogPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("mobile"), querySendStatus(c))
	writePage(c, page, err)
}

func (h *handler) smsLogGet(c *gin.Context, _ caller) {
	item, err := h.db.SmsLogByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, &item, err)
}

func bind(c *gin.Context, dest any) bool {
	if err := c.ShouldBindJSON(dest); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return false
	}
	return true
}

func writeID(c *gin.Context, id int64, err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if c.Request.Method == http.MethodPost || strings.HasPrefix(c.Request.URL.Path, "/admin-api/system/notify-template/send") || strings.Contains(c.Request.URL.Path, "/send-") {
		if id == 0 {
			httpx.OK(c, nil)
			return
		}
		httpx.OK(c, id)
		return
	}
	httpx.OK(c, true)
}

func writePage[T any](c *gin.Context, page Page[T], err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if page.List == nil {
		page.List = []T{}
	}
	httpx.OK(c, page)
}

func writeList[T any](c *gin.Context, list []T, err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if list == nil {
		list = []T{}
	}
	httpx.OK(c, list)
}

func writeOne[T any](c *gin.Context, item *T, err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func writeBiz(c *gin.Context, err error) {
	if biz, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	writeAuth(c, err)
}

func writeAuth(c *gin.Context, err error) {
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

func queryInt(c *gin.Context, name string) int64 {
	n, _ := strconv.ParseInt(c.Query(name), 10, 64)
	return n
}

func queryIDs(c *gin.Context) []int64 {
	var ids []int64
	for _, text := range c.QueryArray("ids") {
		for _, part := range strings.Split(text, ",") {
			if part == "" {
				continue
			}
			ids = append(ids, int64(atoi(part, 0)))
		}
	}
	return ids
}

func queryStatus(c *gin.Context) *int { return queryOptionalInt(c, "status") }

func querySendStatus(c *gin.Context) *int { return queryOptionalInt(c, "sendStatus") }

func queryOptionalInt(c *gin.Context, name string) *int {
	text := c.Query(name)
	if text == "" {
		return nil
	}
	value := atoi(text, 0)
	return &value
}

func queryBool(c *gin.Context, name string) *bool {
	switch c.Query(name) {
	case "true", "1":
		v := true
		return &v
	case "false", "0":
		v := false
		return &v
	default:
		return nil
	}
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
