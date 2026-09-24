package identity

import (
	"net/url"
	"strconv"
)

// AuthorizeURL 拼出浏览器要跳过去的社交授权地址。
func AuthorizeURL(socialType int, clientID, redirectURI, state string) (string, error) {
	if clientID == "" || redirectURI == "" {
		return "", &Error{Code: 400, Msg: "社交客户端和回调地址不能为空"}
	}
	query := url.Values{}
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	query.Set("response_type", "code")
	switch socialType {
	case 10:
		query.Set("client_id", clientID)
		return "https://gitee.com/oauth/authorize?" + query.Encode(), nil
	case 20:
		query.Set("client_id", clientID)
		query.Set("scope", "openid")
		query.Set("prompt", "consent")
		return "https://login.dingtalk.com/oauth2/auth?" + query.Encode(), nil
	case 30:
		query.Set("appid", clientID)
		query.Set("scope", "snsapi_base")
		return "https://open.work.weixin.qq.com/wwopen/sso/qrConnect?" + query.Encode(), nil
	case 31:
		query.Set("appid", clientID)
		query.Set("scope", "snsapi_userinfo")
		return "https://open.weixin.qq.com/connect/oauth2/authorize?" + query.Encode() + "#wechat_redirect", nil
	case 32:
		query.Set("appid", clientID)
		query.Set("scope", "snsapi_login")
		return "https://open.weixin.qq.com/connect/qrconnect?" + query.Encode() + "#wechat_redirect", nil
	case 34:
		return "", &Error{Code: 400, Msg: "微信小程序不使用浏览器跳转授权"}
	default:
		return "", &Error{Code: 400, Msg: "不支持的社交平台 " + strconv.Itoa(socialType)}
	}
}
