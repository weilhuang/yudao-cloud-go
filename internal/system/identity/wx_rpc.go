package identity

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// mountWxRPC 挂上微信公众号和小程序 Feign。每条都会访问微信，失败时返回业务错误。
func mountWxRPC(r *gin.Engine, h *socialRPC) {
	client := r.Group("/rpc-api/system/social-client")
	client.GET("/create-wx-mp-jsapi-signature", h.jsapi)
	client.GET("/create-wx-ma-phone-number-info", h.phone)
	client.GET("/get-wxa-qrcode", h.qrcode)
	client.GET("/get-wxa-subscribe-template-list", h.templates)
	client.POST("/send-wxa-subscribe-message", h.subscribe)
	client.POST("/upload-wxa-order-shipping-info", h.shipping)
	client.POST("/notify-wxa-order-confirm-receive", h.confirm)
}

func (h *socialRPC) jsapi(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	pageURL, ok := rpcQueryText(c, "url")
	if !ok {
		return
	}
	sign, err := h.svc.WxJsapiSignature(c.Request.Context(), tenantID, userType, pageURL)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, sign)
}

func (h *socialRPC) phone(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	code, ok := rpcQueryText(c, "phoneCode")
	if !ok {
		return
	}
	info, err := h.svc.WxPhoneNumber(c.Request.Context(), tenantID, userType, code)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, info)
}

func (h *socialRPC) qrcode(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	scene, ok := rpcQueryText(c, "scene")
	if !ok {
		return
	}
	if scene == "" {
		rpcFail(c, 400, "场景不能为空")
		return
	}
	path, ok := rpcQueryText(c, "path")
	if !ok {
		return
	}
	if path == "" {
		rpcFail(c, 400, "页面路径不能为空")
		return
	}
	width, ok := rpcQueryOptionalInt(c, "width")
	if !ok {
		return
	}
	autoColor, ok := rpcQueryOptionalBool(c, "autoColor")
	if !ok {
		return
	}
	checkPath, ok := rpcQueryOptionalBool(c, "checkPath")
	if !ok {
		return
	}
	hyaline, ok := rpcQueryOptionalBool(c, "hyaline")
	if !ok {
		return
	}
	raw, err := h.svc.WxaQrcode(c.Request.Context(), tenantID, scene, path, width, autoColor, checkPath, hyaline)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, raw)
}

func (h *socialRPC) templates(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	list, err := h.svc.WxaTemplates(c.Request.Context(), tenantID, userType)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *socialRPC) subscribe(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	req, ok := bindSubscribe(c)
	if !ok {
		return
	}
	sent, err := h.svc.SendWxaSubscribe(c.Request.Context(), tenantID, req)
	if err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, sent)
}

func (h *socialRPC) shipping(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	req, ok := bindShipping(c)
	if !ok {
		return
	}
	if err := h.svc.UploadWxaShipping(c.Request.Context(), tenantID, userType, req); err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *socialRPC) confirm(c *gin.Context) {
	tenantID, ok := h.tenant(c)
	if !ok {
		return
	}
	userType, ok := rpcQueryInt(c, "userType")
	if !ok {
		return
	}
	var req struct {
		TransactionID *string `json:"transactionId"`
		ReceivedTime  *int64  `json:"receivedTime"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rpcFail(c, 400, "请求参数不正确")
		return
	}
	if req.TransactionID == nil || *req.TransactionID == "" {
		rpcFail(c, 400, "原支付交易对应的微信订单号不能为空")
		return
	}
	if req.ReceivedTime == nil {
		rpcFail(c, 400, "快递签收时间不能为空")
		return
	}
	if err := h.svc.NotifyWxaConfirm(c.Request.Context(), tenantID, userType, *req.TransactionID, *req.ReceivedTime); err != nil {
		rpcFailErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) sendSubscribe(c *gin.Context, who caller) {
	req, ok := bindSubscribe(c)
	if !ok {
		return
	}
	sent, err := h.svc.SendWxaSubscribe(c.Request.Context(), who.tenantID, req)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, sent)
}

func bindSubscribe(c *gin.Context) (WxaSubscribe, bool) {
	var req struct {
		UserID        *int64            `json:"userId"`
		UserType      *int              `json:"userType"`
		TemplateTitle *string           `json:"templateTitle"`
		Page          string            `json:"page"`
		Messages      map[string]string `json:"messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rpcFail(c, 400, "请求参数不正确")
		return WxaSubscribe{}, false
	}
	if req.UserID == nil {
		rpcFail(c, 400, "用户编号不能为空")
		return WxaSubscribe{}, false
	}
	if req.UserType == nil {
		rpcFail(c, 400, "用户类型不能为空")
		return WxaSubscribe{}, false
	}
	if _, ok := rpcUserTypes[*req.UserType]; !ok {
		rpcFail(c, 400, userTypeRangeMsg)
		return WxaSubscribe{}, false
	}
	if req.TemplateTitle == nil || *req.TemplateTitle == "" {
		rpcFail(c, 400, "消息模版标题不能为空")
		return WxaSubscribe{}, false
	}
	return WxaSubscribe{UserID: *req.UserID, UserType: *req.UserType, TemplateTitle: *req.TemplateTitle, Page: req.Page, Messages: req.Messages}, true
}

func bindShipping(c *gin.Context) (WxaShipping, bool) {
	var req struct {
		OpenID          *string `json:"openid"`
		TransactionID   *string `json:"transactionId"`
		LogisticsType   *int    `json:"logisticsType"`
		LogisticsNo     string  `json:"logisticsNo"`
		ExpressCompany  string  `json:"expressCompany"`
		ItemDesc        *string `json:"itemDesc"`
		ReceiverContact *string `json:"receiverContact"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rpcFail(c, 400, "请求参数不正确")
		return WxaShipping{}, false
	}
	if req.OpenID == nil || *req.OpenID == "" {
		rpcFail(c, 400, "支付者，支付者信息(openid)不能为空")
		return WxaShipping{}, false
	}
	if req.TransactionID == nil || *req.TransactionID == "" {
		rpcFail(c, 400, "原支付交易对应的微信订单号不能为空")
		return WxaShipping{}, false
	}
	if req.LogisticsType == nil {
		rpcFail(c, 400, "物流模式不能为空")
		return WxaShipping{}, false
	}
	if req.ItemDesc == nil || *req.ItemDesc == "" {
		rpcFail(c, 400, "商品信息不能为空")
		return WxaShipping{}, false
	}
	if req.ReceiverContact == nil || *req.ReceiverContact == "" {
		rpcFail(c, 400, "收件人手机号")
		return WxaShipping{}, false
	}
	return WxaShipping{
		OpenID: *req.OpenID, TransactionID: *req.TransactionID, LogisticsType: *req.LogisticsType,
		LogisticsNo: req.LogisticsNo, ExpressCompany: req.ExpressCompany, ItemDesc: *req.ItemDesc,
		ReceiverContact: *req.ReceiverContact,
	}, true
}

func rpcQueryOptionalInt(c *gin.Context, name string) (*int, bool) {
	raw, present := c.GetQuery(name)
	if !present || raw == "" {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		rpcFail(c, 400, "请求参数类型错误:"+name)
		return nil, false
	}
	return &value, true
}

func rpcQueryOptionalBool(c *gin.Context, name string) (*bool, bool) {
	raw, present := c.GetQuery(name)
	if !present || raw == "" {
		return nil, true
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		rpcFail(c, 400, "请求参数类型错误:"+name)
		return nil, false
	}
	return &value, true
}
