package apilog

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/url"
	"strings"
)

const (
	omittedRequest  = "[请求体已省略]"
	omittedQuery    = "[查询参数已省略]"
	omittedResponse = "[响应体已省略]"
	omittedAuth     = "[认证响应已省略]"
)

// 敏感字段按语义识别，兼容 camelCase、snake_case 和大小写差异。
func sensitiveKey(key string, query bool) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	if strings.Contains(normalized, "password") || strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") || strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "apikey") {
		return true
	}
	switch normalized {
	case "authorization", "captchaverification", "verificationcode", "smscode", "emailcode",
		"authcode", "ticket", "state", "redirecturi", "redirecturl", "callbackurl":
		return true
	case "code":
		// OAuth 回调的 query code 是凭据；响应体顶层的业务 code 要保留。
		return query
	}
	return false
}

func redactValues(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return omittedQuery
	}
	for key, items := range values {
		if sensitiveKey(key, true) {
			for i := range items {
				items[i] = "*"
			}
			continue
		}
		for i, item := range items {
			if hasEmbeddedSecret(item) {
				items[i] = "*"
			}
		}
	}
	return values.Encode()
}

func safeRequestBody(contentType string, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if len(raw) > 8192 {
		return omittedRequest
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/x-www-form-urlencoded" {
		return redactValues(string(raw))
	}
	trimmed := bytes.TrimSpace(raw)
	if mediaType == "application/json" || (len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')) {
		if safe, ok := redactJSON(raw); ok {
			return safe
		}
	}
	return omittedRequest
}

func safeResponse(path string, raw []byte, truncated bool) string {
	if sensitiveAuthPath(path) {
		return omittedAuth
	}
	if len(raw) == 0 {
		return ""
	}
	// 被截断或无法解析的内容不能原样落库；令牌可能恰好落在截断边缘。
	if truncated {
		return omittedResponse
	}
	if safe, ok := redactJSON(raw); ok {
		return safe
	}
	return omittedResponse
}

func sensitiveAuthPath(path string) bool {
	return strings.Contains(path, "/auth/") || strings.Contains(path, "/oauth2/open/")
}

func redactJSON(raw []byte) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "", false
	}
	// 只保留有字段名可供脱敏的 JSON；原始字符串和数字没有可靠的字段语义。
	switch value.(type) {
	case map[string]any, []any:
	default:
		return "", false
	}
	encoded, err := json.Marshal(redactJSONValue(value))
	return string(encoded), err == nil
}

func redactJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sensitiveKey(key, false) {
				typed[key] = "*"
			} else {
				typed[key] = redactJSONValue(child)
			}
		}
	case []any:
		for i, child := range typed {
			typed[i] = redactJSONValue(child)
		}
	case string:
		if hasEmbeddedSecret(typed) {
			return "*"
		}
	}
	return value
}

func hasEmbeddedSecret(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "bearer ") || strings.Contains(lower, "token=") ||
		strings.Contains(lower, "password=") || strings.Contains(lower, "secret=") ||
		strings.Contains(lower, "code=")
}
