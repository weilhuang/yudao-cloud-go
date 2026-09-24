package message

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// dingTalkSend 用钉钉自定义机器人模拟短信。apiKey 是 access_token，apiSecret 用来签名。
func dingTalkSend(ctx context.Context, client *http.Client, base, apiKey, apiSecret, mobile string, logID int64, params map[string]any) (string, string, string, bool, error) {
	if apiKey == "" || apiSecret == "" {
		return "", "", "", false, fmt.Errorf("钉钉机器人的 apiKey 和 apiSecret 不能为空")
	}
	if base == "" {
		base = "https://oapi.dingtalk.com"
	}
	if client == nil {
		client = http.DefaultClient
	}
	timestamp := time.Now().UnixMilli()
	mac := hmac.New(sha256.New, []byte(apiSecret))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "\n" + apiSecret))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	endpoint := fmt.Sprintf("%s/robot/send?access_token=%s&timestamp=%d&sign=%s", base, url.QueryEscape(apiKey), timestamp, sign)
	raw, _ := json.Marshal(params)
	body, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": fmt.Sprintf("【模拟短信】\n手机号：%s\n短信日志编号：%d\n模板参数：%s", mobile, logID, raw)},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", false, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(payload, &result)
	return strconv.Itoa(result.ErrCode), result.ErrMsg, fmt.Sprintf("ding-%d", logID), result.ErrCode == 0, nil
}
