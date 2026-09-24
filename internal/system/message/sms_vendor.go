package message

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type smsResult struct {
	code   string
	msg    string
	serial string
	ok     bool
}

// sendVendorSMS 按渠道编码调用对应云厂商。base 为空时用官方地址，测试可以换成本地服务。
func sendVendorSMS(ctx context.Context, client *http.Client, channel SmsChannel, template SmsTemplate, mobile string, logID int64, params map[string]any, bases smsBases) (smsResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	switch channel.Code {
	case "ALIYUN":
		return sendAliyun(ctx, client, bases.aliyun, channel, template, mobile, logID, params)
	case "TENCENT":
		return sendTencent(ctx, client, bases.tencent, channel, template, mobile, params)
	case "HUAWEI":
		return sendHuawei(ctx, client, bases.huawei, channel, template, mobile, logID, params)
	case "QINIU":
		return sendQiniu(ctx, client, bases.qiniu, channel, template, mobile, logID, params)
	default:
		return smsResult{}, fmt.Errorf("未知短信渠道 %s", channel.Code)
	}
}

type smsBases struct {
	aliyun, tencent, huawei, qiniu string
}

func sendAliyun(ctx context.Context, client *http.Client, base string, channel SmsChannel, template SmsTemplate, mobile string, logID int64, params map[string]any) (smsResult, error) {
	if base == "" {
		base = "https://dysmsapi.aliyuncs.com"
	}
	query := map[string]string{
		"PhoneNumbers":  mobile,
		"SignName":      channel.Signature,
		"TemplateCode":  template.APITemplateID,
		"TemplateParam": string(mustJSON(params)),
		"OutId":         fmt.Sprint(logID),
	}
	body, headers, err := aliyunSigned("SendSms", channel.APIKey, channel.APISecret, query, hostOf(base))
	if err != nil {
		return smsResult{}, err
	}
	raw, err := doBody(ctx, client, http.MethodPost, base+"?"+body, headers, nil)
	if err != nil {
		return smsResult{}, err
	}
	var resp struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		BizID     string `json:"BizId"`
		RequestID string `json:"RequestId"`
	}
	_ = json.Unmarshal(raw, &resp)
	return smsResult{code: resp.Code, msg: resp.Message, serial: resp.BizID, ok: resp.Code == "OK"}, nil
}

func aliyunSigned(action, key, secret string, query map[string]string, host string) (string, http.Header, error) {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, percentCode(k)+"="+percentCode(query[k]))
	}
	queryString := strings.Join(parts, "&")
	payloadHash := sha256Hex("")
	date := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	headers := map[string]string{
		"host":                  host,
		"x-acs-action":          action,
		"x-acs-content-sha256":  payloadHash,
		"x-acs-date":            date,
		"x-acs-signature-nonce": nonce,
		"x-acs-version":         "2017-05-25",
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonical, signed strings.Builder
	for i, name := range names {
		canonical.WriteString(name + ":" + strings.TrimSpace(headers[name]) + "\n")
		if i > 0 {
			signed.WriteByte(';')
		}
		signed.WriteString(name)
	}
	canonicalRequest := "POST\n/\n" + queryString + "\n" + canonical.String() + "\n" + signed.String() + "\n" + payloadHash
	stringToSign := "ACS3-HMAC-SHA256\n" + sha256Hex(canonicalRequest)
	signature := hmacSHA256Hex([]byte(secret), stringToSign)
	h := http.Header{}
	for name, value := range headers {
		h.Set(name, value)
	}
	h.Set("Authorization", "ACS3-HMAC-SHA256 Credential="+key+", SignedHeaders="+signed.String()+", Signature="+signature)
	return queryString, h, nil
}

func sendTencent(ctx context.Context, client *http.Client, base string, channel SmsChannel, template SmsTemplate, mobile string, params map[string]any) (smsResult, error) {
	parts := strings.Fields(channel.APIKey)
	if len(parts) != 2 {
		return smsResult{}, fmt.Errorf("腾讯云短信 apiKey 配置格式错误，请配置为[secretId sdkAppId]")
	}
	if base == "" {
		base = "https://sms.tencentcloudapi.com"
	}
	values := orderedValues(template.Params, params)
	payload := map[string]any{
		"PhoneNumberSet":   []string{mobile},
		"SmsSdkAppId":      parts[1],
		"SignName":         channel.Signature,
		"TemplateId":       template.APITemplateID,
		"TemplateParamSet": values,
	}
	rawBody, _ := json.Marshal(payload)
	now := time.Now().UTC()
	timestamp := fmt.Sprint(now.Unix())
	date := now.Format("2006-01-02")
	action := "SendSms"
	canonicalHeaders := "content-type:application/json; charset=utf-8\nhost:" + hostOf(base) + "\nx-tc-action:" + strings.ToLower(action) + "\n"
	signedHeaders := "content-type;host;x-tc-action"
	canonical := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex(string(rawBody))
	scope := date + "/sms/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + sha256Hex(canonical)
	secretDate := hmacSHA256([]byte("TC3"+channel.APISecret), date)
	secretService := hmacSHA256(secretDate, "sms")
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	header := http.Header{}
	header.Set("Content-Type", "application/json; charset=utf-8")
	header.Set("Host", hostOf(base))
	header.Set("X-TC-Action", action)
	header.Set("X-TC-Timestamp", timestamp)
	header.Set("X-TC-Version", "2021-01-11")
	header.Set("X-TC-Region", "ap-guangzhou")
	header.Set("Authorization", "TC3-HMAC-SHA256 Credential="+parts[0]+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	raw, err := doBody(ctx, client, http.MethodPost, base, header, rawBody)
	if err != nil {
		return smsResult{}, err
	}
	var resp struct {
		Response struct {
			RequestID string `json:"RequestId"`
			Error     *struct {
				Code, Message string
			} `json:"Error"`
			SendStatusSet []struct {
				Code, Message, SerialNo string
			} `json:"SendStatusSet"`
		} `json:"Response"`
	}
	_ = json.Unmarshal(raw, &resp)
	if resp.Response.Error != nil {
		return smsResult{code: resp.Response.Error.Code, msg: resp.Response.Error.Message}, nil
	}
	if len(resp.Response.SendStatusSet) == 0 {
		return smsResult{code: "EMPTY", msg: "腾讯云没有返回发送结果"}, nil
	}
	item := resp.Response.SendStatusSet[0]
	return smsResult{code: item.Code, msg: item.Message, serial: item.SerialNo, ok: item.Code == "Ok"}, nil
}

func sendHuawei(ctx context.Context, client *http.Client, base string, channel SmsChannel, template SmsTemplate, mobile string, logID int64, params map[string]any) (smsResult, error) {
	parts := strings.Fields(channel.APIKey)
	if len(parts) != 2 {
		return smsResult{}, fmt.Errorf("华为云短信 apiKey 配置格式错误，请配置为[accessKeyId sender]")
	}
	if base == "" {
		// 主机名带 :443，和 Java 客户端签名时用的 host 一致。
		base = "https://smsapi.cn-north-4.myhuaweicloud.com:443/sms/batchSendSms/v1"
	}
	form := "from=" + url.QueryEscape(parts[1]) + "&to=" + url.QueryEscape(mobile) + "&templateId=" + url.QueryEscape(template.APITemplateID) +
		"&templateParas=" + url.QueryEscape(string(mustJSON(orderedValues(template.Params, params)))) +
		"&statusCallback=" + url.QueryEscape(channel.CallbackURL) + "&extend=" + url.QueryEscape(fmt.Sprint(logID))
	sdkDate := time.Now().UTC().Format("20060102T150405Z")
	host := hostOf(base)
	canonicalHeaders := "content-type:application/x-www-form-urlencoded\nhost:" + host + "\nx-sdk-date:" + sdkDate + "\n"
	signed := "content-type;host;x-sdk-date"
	uri := pathOf(base)
	canonical := "POST\n" + uri + "\n\n" + canonicalHeaders + "\n" + signed + "\n" + sha256Hex(form)
	stringToSign := "SDK-HMAC-SHA256\n" + sdkDate + "\n" + sha256Hex(canonical)
	signature := hmacSHA256Hex([]byte(channel.APISecret), stringToSign)
	header := http.Header{}
	header.Set("Content-Type", "application/x-www-form-urlencoded")
	header.Set("Host", host)
	header.Set("X-Sdk-Date", sdkDate)
	header.Set("Authorization", "SDK-HMAC-SHA256 Access="+parts[0]+", SignedHeaders="+signed+", Signature="+signature)
	raw, err := doBody(ctx, client, http.MethodPost, base, header, []byte(form))
	if err != nil {
		return smsResult{}, err
	}
	var resp struct {
		Code   string `json:"code"`
		Desc   string `json:"description"`
		Result []struct {
			SmsMsgID string `json:"smsMsgId"`
			Status   string `json:"status"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &resp)
	if len(resp.Result) == 0 {
		return smsResult{code: resp.Code, msg: resp.Desc, ok: false}, nil
	}
	return smsResult{code: resp.Result[0].Status, msg: resp.Desc, serial: resp.Result[0].SmsMsgID, ok: resp.Code == "000000"}, nil
}

func sendQiniu(ctx context.Context, client *http.Client, base string, channel SmsChannel, template SmsTemplate, mobile string, logID int64, params map[string]any) (smsResult, error) {
	if base == "" {
		base = "https://sms.qiniuapi.com"
	}
	path := "/v1/message/single"
	// 字段顺序和 Java LinkedHashMap 一致，七牛按原文验签。
	rawBody, _ := json.Marshal(struct {
		TemplateID string         `json:"template_id"`
		Mobile     string         `json:"mobile"`
		Parameters map[string]any `json:"parameters"`
		Seq        string         `json:"seq"`
	}{template.APITemplateID, mobile, params, fmt.Sprint(logID)})
	signDate := time.Now().UTC().Format("20060102T150405Z")
	data := "POST " + path + "\nHost: " + hostOf(base) + "\nContent-Type: application/json\nX-Qiniu-Date: " + signDate + "\n\n" + string(rawBody)
	mac := hmac.New(sha1.New, []byte(channel.APISecret))
	_, _ = mac.Write([]byte(data))
	signature := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	header := http.Header{}
	header.Set("Host", hostOf(base))
	header.Set("Content-Type", "application/json")
	header.Set("X-Qiniu-Date", signDate)
	header.Set("Authorization", "Qiniu "+channel.APIKey+":"+signature)
	raw, err := doBody(ctx, client, http.MethodPost, strings.TrimRight(base, "/")+path, header, rawBody)
	if err != nil {
		return smsResult{}, err
	}
	var resp struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		MessageID string `json:"message_id"`
	}
	_ = json.Unmarshal(raw, &resp)
	if resp.Error != "" {
		return smsResult{code: resp.Error, msg: resp.Message}, nil
	}
	return smsResult{serial: resp.MessageID, ok: resp.MessageID != ""}, nil
}

func orderedValues(names []string, params map[string]any) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprint(params[name]))
	}
	return out
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func percentCode(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msg))
	return mac.Sum(nil)
}

func hmacSHA256Hex(key []byte, msg string) string {
	return hex.EncodeToString(hmacSHA256(key, msg))
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

func pathOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "/"
	}
	if !strings.HasSuffix(u.Path, "/") {
		return u.Path + "/"
	}
	return u.Path
}

func doBody(ctx context.Context, client *http.Client, method, endpoint string, header http.Header, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
