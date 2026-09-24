package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	socialWechatMP    = 31
	socialWechatMini  = 34
	wxPayOrderMissing = 10060001
)

// 与 Java yudao.wxa-code.env-version、yudao.wxa-subscribe-message.miniprogram-state 的默认值一致。
const (
	defaultWxEnv   = "release"
	defaultWxState = "formal"
)

// JsapiSignature 对齐微信公众号 JS-SDK 签名。
type JsapiSignature struct {
	AppID     string `json:"appId"`
	NonceStr  string `json:"nonceStr"`
	Timestamp int64  `json:"timestamp"`
	URL       string `json:"url"`
	Signature string `json:"signature"`
}

// WxPhone 是小程序手机号。区号按字符串返回，和 Java DTO 一致。
type WxPhone struct {
	PhoneNumber     string `json:"phoneNumber"`
	PurePhoneNumber string `json:"purePhoneNumber"`
	CountryCode     string `json:"countryCode"`
}

// WxTemplate 是小程序订阅模板。id 对应微信的 priTmplId。
type WxTemplate struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Example string `json:"example"`
	Type    int    `json:"type"`
}

// WxaSubscribe 是发送订阅消息的请求。
type WxaSubscribe struct {
	UserID        int64
	UserType      int
	TemplateTitle string
	Page          string
	Messages      map[string]string
}

// WxaShipping 是上传发货信息的请求。物流模式 1 是快递，会脱敏收件人手机号。
type WxaShipping struct {
	OpenID          string
	TransactionID   string
	LogisticsType   int
	LogisticsNo     string
	ExpressCompany  string
	ItemDesc        string
	ReceiverContact string
}

type wxCachedToken struct {
	token   string
	expires time.Time
}

func (s *Service) wxBase() string {
	if s != nil && s.WechatBase != "" {
		return strings.TrimRight(s.WechatBase, "/")
	}
	return "https://api.weixin.qq.com"
}

func (s *Service) wxHTTP() *http.Client {
	if s != nil && s.HTTP != nil {
		return s.HTTP
	}
	return http.DefaultClient
}

func (s *Service) wxNow() time.Time {
	if s != nil && s.WxNow != nil {
		return s.WxNow()
	}
	return time.Now()
}

func (s *Service) wxNonce() string {
	if s != nil && s.WxNonce != nil {
		return s.WxNonce()
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "0123456789abcdef"
	}
	return hex.EncodeToString(buf)
}

func (s *Service) wxSleep(d time.Duration) {
	if s != nil && s.WxSleep != nil {
		s.WxSleep(d)
		return
	}
	time.Sleep(d)
}

func (s *Service) wxEnv() string {
	if s != nil && s.WxEnvVersion != "" {
		return s.WxEnvVersion
	}
	return defaultWxEnv
}

func (s *Service) wxState() string {
	if s != nil && s.WxMiniState != "" {
		return s.WxMiniState
	}
	return defaultWxState
}

// WxJsapiSignature 用公众号 jsapi_ticket 按微信规则签名。没有启用的公众号配置时返回社交客户端不存在。
func (s *Service) WxJsapiSignature(ctx context.Context, tenantID int64, userType int, pageURL string) (*JsapiSignature, error) {
	client, err := s.enabledClient(ctx, tenantID, socialWechatMP, userType)
	if err != nil {
		return nil, err
	}
	ticket, err := s.wxTicket(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return nil, err
	}
	ts := s.wxNow().Unix()
	nonce := s.wxNonce()
	sign := wxSHA1(
		"jsapi_ticket="+ticket,
		"noncestr="+nonce,
		"timestamp="+strconv.FormatInt(ts, 10),
		"url="+pageURL,
	)
	return &JsapiSignature{AppID: client.ClientID, NonceStr: nonce, Timestamp: ts, URL: pageURL, Signature: sign}, nil
}

// WxPhoneNumber 用手机号授权码向微信换手机号。
func (s *Service) WxPhoneNumber(ctx context.Context, tenantID int64, userType int, phoneCode string) (*WxPhone, error) {
	client, err := s.enabledClient(ctx, tenantID, socialWechatMini, userType)
	if err != nil {
		return nil, err
	}
	token, err := s.wxAccessToken(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return nil, wxBiz(1_002_018_200, "获得手机号失败")
	}
	raw, err := s.wxPost(ctx, "/wxa/business/getuserphonenumber", token, map[string]string{"code": phoneCode})
	if err != nil {
		return nil, wxBiz(1_002_018_200, "获得手机号失败")
	}
	var body struct {
		ErrCode   int `json:"errcode"`
		PhoneInfo struct {
			PhoneNumber     string          `json:"phoneNumber"`
			PurePhoneNumber string          `json:"purePhoneNumber"`
			CountryCode     json.RawMessage `json:"countryCode"`
		} `json:"phone_info"`
	}
	if json.Unmarshal(raw, &body) != nil || body.ErrCode != 0 || body.PhoneInfo.PhoneNumber == "" {
		return nil, wxBiz(1_002_018_200, "获得手机号失败")
	}
	return &WxPhone{
		PhoneNumber: body.PhoneInfo.PhoneNumber, PurePhoneNumber: body.PhoneInfo.PurePhoneNumber,
		CountryCode: rawString(body.PhoneInfo.CountryCode),
	}, nil
}

// WxaQrcode 生成不限制的小程序码。Java 固定用会员类型的小程序配置。
func (s *Service) WxaQrcode(ctx context.Context, tenantID int64, scene, path string, width *int, autoColor, checkPath, hyaline *bool) ([]byte, error) {
	client, err := s.enabledClient(ctx, tenantID, socialWechatMini, 1)
	if err != nil {
		return nil, err
	}
	token, err := s.wxAccessToken(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return nil, wxBiz(1_002_018_201, "获得小程序码失败")
	}
	body := map[string]any{
		"scene":       scene,
		"check_path":  boolOr(checkPath, true),
		"env_version": s.wxEnv(),
		"width":       intOr(width, 430),
		"auto_color":  boolOr(autoColor, true),
		"is_hyaline":  boolOr(hyaline, true),
	}
	if path != "" {
		body["page"] = path
	}
	raw, kind, err := s.wxPostRaw(ctx, "/wxa/getwxacodeunlimit", token, body)
	if err != nil || looksJSON(kind, raw) {
		return nil, wxBiz(1_002_018_201, "获得小程序码失败")
	}
	return raw, nil
}

// WxaTemplates 读取小程序订阅模板。微信失败时不返回空列表冒充成功。
func (s *Service) WxaTemplates(ctx context.Context, tenantID int64, userType int) ([]WxTemplate, error) {
	client, err := s.enabledClient(ctx, tenantID, socialWechatMini, userType)
	if err != nil {
		return nil, err
	}
	token, err := s.wxAccessToken(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return nil, wxBiz(1_002_018_202, "获得小程序订阅消息模版失败")
	}
	raw, err := s.wxGet(ctx, "/wxaapi/newtmpl/gettemplate?access_token="+url.QueryEscape(token))
	if err != nil {
		return nil, wxBiz(1_002_018_202, "获得小程序订阅消息模版失败")
	}
	var body struct {
		ErrCode int `json:"errcode"`
		Data    []struct {
			PriTmplID string `json:"priTmplId"`
			Title     string `json:"title"`
			Content   string `json:"content"`
			Example   string `json:"example"`
			Type      int    `json:"type"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil || body.ErrCode != 0 {
		return nil, wxBiz(1_002_018_202, "获得小程序订阅消息模版失败")
	}
	list := make([]WxTemplate, 0, len(body.Data))
	for _, item := range body.Data {
		list = append(list, WxTemplate{ID: item.PriTmplID, Title: item.Title, Content: item.Content, Example: item.Example, Type: item.Type})
	}
	return list, nil
}

// SendWxaSubscribe 按模板标题找到 priTmplId，再按小程序 openid 发送。
// 没有模板或没有 openid 时返回 false，不当成系统错误。微信发送失败才返回业务错误。
func (s *Service) SendWxaSubscribe(ctx context.Context, tenantID int64, req WxaSubscribe) (bool, error) {
	list, err := s.WxaTemplates(ctx, tenantID, req.UserType)
	if err != nil {
		return false, err
	}
	templateID := ""
	for _, item := range list {
		if item.Title == req.TemplateTitle {
			templateID = item.ID
			break
		}
	}
	if templateID == "" {
		return false, nil
	}
	user, err := s.Store.SocialByBind(ctx, tenantID, req.UserID, req.UserType, socialWechatMini)
	if err != nil {
		return false, err
	}
	if user == nil || strings.TrimSpace(user.OpenID) == "" {
		return false, nil
	}
	client, err := s.enabledClient(ctx, tenantID, socialWechatMini, req.UserType)
	if err != nil {
		return false, err
	}
	token, err := s.wxAccessToken(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return false, wxBiz(1_002_018_203, "发送小程序订阅消息失败")
	}
	data := map[string]map[string]string{}
	for key, value := range req.Messages {
		data[key] = map[string]string{"value": value}
	}
	payload := map[string]any{
		"touser": user.OpenID, "template_id": templateID,
		"miniprogram_state": s.wxState(), "lang": "zh_CN", "data": data,
	}
	if req.Page != "" {
		payload["page"] = req.Page
	}
	raw, err := s.wxPost(ctx, "/cgi-bin/message/subscribe/send", token, payload)
	if err != nil || wxErrCode(raw) != 0 {
		return false, wxBiz(1_002_018_203, "发送小程序订阅消息失败")
	}
	return true, nil
}

// UploadWxaShipping 上传发货信息。支付单尚不存在（10060001）时按 1 秒、2 秒、4 秒重试。
func (s *Service) UploadWxaShipping(ctx context.Context, tenantID int64, userType int, req WxaShipping) error {
	client, err := s.enabledClient(ctx, tenantID, socialWechatMini, userType)
	if err != nil {
		return err
	}
	token, err := s.wxAccessToken(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return wxBiz(1_002_018_204, "上传微信小程序发货信息失败")
	}
	shipping := map[string]any{"item_desc": req.ItemDesc}
	if req.LogisticsType == 1 {
		shipping["tracking_no"] = req.LogisticsNo
		shipping["express_company"] = req.ExpressCompany
		shipping["contact"] = map[string]string{"receiver_contact": maskMobile(req.ReceiverContact)}
	}
	payload := map[string]any{
		"order_key":      map[string]any{"order_number_type": 2, "transaction_id": req.TransactionID},
		"logistics_type": req.LogisticsType,
		"delivery_mode":  1,
		"shipping_list":  []any{shipping},
		"payer":          map[string]string{"openid": req.OpenID},
		"upload_time":    s.wxNow().In(shanghai()).Format("2006-01-02T15:04:05.000Z07:00"),
	}
	backoffs := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		raw, err := s.wxPost(ctx, "/wxa/sec/order/upload_shipping_info", token, payload)
		code := wxErrCode(raw)
		if err == nil && code == 0 {
			return nil
		}
		if code == wxPayOrderMissing && attempt < len(backoffs) {
			s.wxSleep(backoffs[attempt])
			continue
		}
		return wxBiz(1_002_018_204, "上传微信小程序发货信息失败")
	}
	return wxBiz(1_002_018_204, "上传微信小程序发货信息失败")
}

// NotifyWxaConfirm 把确认收货时间通知给微信。receivedTime 是毫秒时间戳，微信要秒。
func (s *Service) NotifyWxaConfirm(ctx context.Context, tenantID int64, userType int, transactionID string, receivedMillis int64) error {
	client, err := s.enabledClient(ctx, tenantID, socialWechatMini, userType)
	if err != nil {
		return err
	}
	token, err := s.wxAccessToken(ctx, client.ClientID, client.ClientSecret)
	if err != nil {
		return wxBiz(1_002_018_205, "上传微信小程序订单收货信息失败")
	}
	raw, err := s.wxPost(ctx, "/wxa/sec/order/notify_confirm_receive", token, map[string]any{
		"transaction_id": transactionID,
		"received_time":  receivedMillis / 1000,
	})
	if err != nil || wxErrCode(raw) != 0 {
		return wxBiz(1_002_018_205, "上传微信小程序订单收货信息失败")
	}
	return nil
}

func (s *Service) wxTicket(ctx context.Context, appID, secret string) (string, error) {
	token, err := s.wxAccessToken(ctx, appID, secret)
	if err != nil {
		return "", err
	}
	raw, err := s.wxGet(ctx, "/cgi-bin/ticket/getticket?access_token="+url.QueryEscape(token)+"&type=jsapi")
	if err != nil {
		return "", err
	}
	var body struct {
		ErrCode int    `json:"errcode"`
		Ticket  string `json:"ticket"`
	}
	if json.Unmarshal(raw, &body) != nil || body.ErrCode != 0 || body.Ticket == "" {
		return "", fmt.Errorf("微信 jsapi_ticket 失败")
	}
	return body.Ticket, nil
}

func (s *Service) wxAccessToken(ctx context.Context, appID, secret string) (string, error) {
	key := appID + "\x00" + secret
	if item, ok := s.wxTokens.Load(key); ok {
		cached := item.(wxCachedToken)
		if s.wxNow().Before(cached.expires) && cached.token != "" {
			return cached.token, nil
		}
	}
	raw, err := s.wxGet(ctx, "/cgi-bin/token?grant_type=client_credential&appid="+url.QueryEscape(appID)+"&secret="+url.QueryEscape(secret))
	if err != nil {
		return "", err
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
	}
	if json.Unmarshal(raw, &body) != nil || body.AccessToken == "" || body.ErrCode != 0 {
		return "", fmt.Errorf("微信 access_token 失败")
	}
	ttl := body.ExpiresIn
	if ttl > 300 {
		ttl -= 200
	}
	if ttl < 1 {
		ttl = 1
	}
	s.wxTokens.Store(key, wxCachedToken{token: body.AccessToken, expires: s.wxNow().Add(time.Duration(ttl) * time.Second)})
	return body.AccessToken, nil
}

func (s *Service) wxGet(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.wxBase()+path, nil)
	if err != nil {
		return nil, err
	}
	raw, _, err := s.wxDo(req)
	return raw, err
}

func (s *Service) wxPost(ctx context.Context, path, token string, payload any) ([]byte, error) {
	raw, _, err := s.wxPostRaw(ctx, path, token, payload)
	return raw, err
}

func (s *Service) wxPostRaw(ctx context.Context, path, token string, payload any) ([]byte, string, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.wxBase()+path+"?access_token="+url.QueryEscape(token), bytes.NewReader(buf))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.wxDo(req)
}

func (s *Service) wxDo(req *http.Request) ([]byte, string, error) {
	resp, err := s.wxHTTP().Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, "", err
	}
	return raw, resp.Header.Get("Content-Type"), nil
}

func wxSHA1(parts ...string) string {
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(sum[:])
}

func wxErrCode(raw []byte) int {
	var body struct {
		ErrCode int `json:"errcode"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return -1
	}
	return body.ErrCode
}

func wxBiz(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func looksJSON(contentType string, raw []byte) bool {
	if strings.Contains(contentType, "json") {
		return true
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func rawString(raw json.RawMessage) string {
	text := strings.Trim(string(raw), `"`)
	if text == "null" {
		return ""
	}
	return text
}

// maskMobile 对齐 Hutool：保留前 3 位和后 4 位。
func maskMobile(num string) string {
	if strings.TrimSpace(num) == "" {
		return ""
	}
	runes := []rune(num)
	start, end := 3, len(runes)-4
	if start > len(runes) || start >= end {
		return num
	}
	for i := start; i < end; i++ {
		runes[i] = '*'
	}
	return string(runes)
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}
