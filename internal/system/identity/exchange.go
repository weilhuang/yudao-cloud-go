package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ExchangeCode 用授权码换 openid。Gitee、钉钉、企业微信、微信公众号、开放平台和小程序都走各自的 HTTP 接口。
func (s *Service) ExchangeCode(ctx context.Context, client SocialClient, code, redirectURI string) (Profile, error) {
	httpClient := s.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	switch client.SocialType {
	case 10:
		return exchangeGitee(ctx, httpClient, s.GiteeBase, client, code, redirectURI)
	case 20:
		return exchangeDingTalk(ctx, httpClient, s.DingTalkBase, client, code)
	case 30:
		return exchangeWeCom(ctx, httpClient, s.WeComBase, client, code)
	case 31, 32:
		return exchangeWechat(ctx, httpClient, s.WechatBase, client, code)
	case 34:
		return exchangeWechatMini(ctx, httpClient, s.WechatBase, client, code)
	default:
		return Profile{}, fmt.Errorf("该平台的授权码兑换尚未接入")
	}
}

func exchangeDingTalk(ctx context.Context, client *http.Client, base string, app SocialClient, code string) (Profile, error) {
	if base == "" {
		base = "https://api.dingtalk.com"
	}
	payload, _ := json.Marshal(map[string]string{
		"clientId": app.ClientID, "clientSecret": app.ClientSecret, "code": code, "grantType": "authorization_code",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1.0/oauth2/userAccessToken", strings.NewReader(string(payload)))
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	tokenBody, err := doJSON(client, req)
	if err != nil {
		return Profile{}, err
	}
	token, _ := tokenBody["accessToken"].(string)
	if token == "" {
		return Profile{}, fmt.Errorf("钉钉没有返回 accessToken")
	}
	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1.0/contact/users/me", nil)
	if err != nil {
		return Profile{}, err
	}
	userReq.Header.Set("x-acs-dingtalk-access-token", token)
	user, err := doJSON(client, userReq)
	if err != nil {
		return Profile{}, err
	}
	openID, _ := user["openId"].(string)
	if openID == "" {
		openID, _ = user["unionId"].(string)
	}
	nick, _ := user["nick"].(string)
	avatar, _ := user["avatarUrl"].(string)
	if openID == "" {
		return Profile{}, fmt.Errorf("钉钉没有返回用户")
	}
	return Profile{OpenID: openID, Nickname: nick, Avatar: avatar}, nil
}

func exchangeWeCom(ctx context.Context, client *http.Client, base string, app SocialClient, code string) (Profile, error) {
	if base == "" {
		base = "https://qyapi.weixin.qq.com"
	}
	tokenURL := fmt.Sprintf("%s/cgi-bin/gettoken?corpid=%s&corpsecret=%s", base, url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret))
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return Profile{}, err
	}
	tokenBody, err := doJSON(client, tokenReq)
	if err != nil {
		return Profile{}, err
	}
	token, _ := tokenBody["access_token"].(string)
	if token == "" {
		return Profile{}, fmt.Errorf("企业微信没有返回 access_token")
	}
	userURL := fmt.Sprintf("%s/cgi-bin/auth/getuserinfo?access_token=%s&code=%s", base, url.QueryEscape(token), url.QueryEscape(code))
	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return Profile{}, err
	}
	user, err := doJSON(client, userReq)
	if err != nil {
		return Profile{}, err
	}
	openID, _ := user["userid"].(string)
	if openID == "" {
		openID, _ = user["openid"].(string)
	}
	if openID == "" {
		return Profile{}, fmt.Errorf("企业微信没有返回用户")
	}
	return Profile{OpenID: openID}, nil
}

func exchangeGitee(ctx context.Context, client *http.Client, base string, app SocialClient, code, redirectURI string) (Profile, error) {
	if base == "" {
		base = "https://gitee.com"
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	form.Set("redirect_uri", redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := doJSON(client, req)
	if err != nil {
		return Profile{}, err
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		return Profile{}, fmt.Errorf("Gitee 没有返回 access_token")
	}
	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v5/user?access_token="+url.QueryEscape(token), nil)
	if err != nil {
		return Profile{}, err
	}
	user, err := doJSON(client, userReq)
	if err != nil {
		return Profile{}, err
	}
	id, _ := user["id"].(float64)
	name, _ := user["name"].(string)
	avatar, _ := user["avatar_url"].(string)
	if id == 0 {
		return Profile{}, fmt.Errorf("Gitee 没有返回用户")
	}
	return Profile{OpenID: fmt.Sprintf("%.0f", id), Nickname: name, Avatar: avatar}, nil
}

func exchangeWechat(ctx context.Context, client *http.Client, base string, app SocialClient, code string) (Profile, error) {
	if base == "" {
		base = "https://api.weixin.qq.com"
	}
	endpoint := fmt.Sprintf("%s/sns/oauth2/access_token?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		base, url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret), url.QueryEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Profile{}, err
	}
	body, err := doJSON(client, req)
	if err != nil {
		return Profile{}, err
	}
	openid, _ := body["openid"].(string)
	if openid == "" {
		msg, _ := body["errmsg"].(string)
		if msg == "" {
			msg = "微信没有返回 openid"
		}
		return Profile{}, fmt.Errorf("%s", msg)
	}
	return Profile{OpenID: openid}, nil
}

func exchangeWechatMini(ctx context.Context, client *http.Client, base string, app SocialClient, code string) (Profile, error) {
	if base == "" {
		base = "https://api.weixin.qq.com"
	}
	endpoint := fmt.Sprintf("%s/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		base, url.QueryEscape(app.ClientID), url.QueryEscape(app.ClientSecret), url.QueryEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Profile{}, err
	}
	body, err := doJSON(client, req)
	if err != nil {
		return Profile{}, err
	}
	openid, _ := body["openid"].(string)
	if openid == "" {
		msg, _ := body["errmsg"].(string)
		if msg == "" {
			msg = "微信小程序没有返回 openid"
		}
		return Profile{}, fmt.Errorf("%s", msg)
	}
	return Profile{OpenID: openid}, nil
}

func doJSON(client *http.Client, req *http.Request) (map[string]any, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}
