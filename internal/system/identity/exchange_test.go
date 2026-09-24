package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExchangeDingTalkAndWeCom(t *testing.T) {
	ding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/userAccessToken":
			_, _ = w.Write([]byte(`{"accessToken":"ding-token"}`))
		case "/v1.0/contact/users/me":
			if r.Header.Get("x-acs-dingtalk-access-token") != "ding-token" {
				t.Fatal(r.Header.Get("x-acs-dingtalk-access-token"))
			}
			_, _ = w.Write([]byte(`{"openId":"ding-user","nick":"钉钉"}`))
		default:
			t.Fatal(r.URL.Path)
		}
	}))
	defer ding.Close()
	svc := &Service{DingTalkBase: ding.URL, HTTP: ding.Client()}
	profile, err := svc.ExchangeCode(context.Background(), SocialClient{SocialType: 20, ClientID: "app", ClientSecret: "sec"}, "code", "")
	if err != nil || profile.OpenID != "ding-user" || profile.Nickname != "钉钉" {
		t.Fatal(profile, err)
	}

	wecom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_, _ = w.Write([]byte(`{"access_token":"wx-token"}`))
		case "/cgi-bin/auth/getuserinfo":
			if r.URL.Query().Get("code") != "wx-code" {
				t.Fatal(r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"userid":"wecom-user"}`))
		default:
			t.Fatal(r.URL.Path)
		}
	}))
	defer wecom.Close()
	svc = &Service{WeComBase: wecom.URL, HTTP: wecom.Client()}
	profile, err = svc.ExchangeCode(context.Background(), SocialClient{SocialType: 30, ClientID: "corp", ClientSecret: "sec"}, "wx-code", "")
	if err != nil || profile.OpenID != "wecom-user" {
		t.Fatal(profile, err)
	}
}

func TestExchangeWechatMiniProgram(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sns/jscode2session" || r.URL.Query().Get("js_code") != "mini-code" {
			t.Fatal(r.URL.String())
		}
		_, _ = w.Write([]byte(`{"openid":"mini-user"}`))
	}))
	defer server.Close()
	svc := &Service{WechatBase: server.URL, HTTP: server.Client()}
	profile, err := svc.ExchangeCode(context.Background(), SocialClient{SocialType: 34, ClientID: "app", ClientSecret: "sec"}, "mini-code", "")
	if err != nil || profile.OpenID != "mini-user" {
		t.Fatal(profile, err)
	}
}

func TestExchangeWechatOpenID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("code") != "auth-code" {
			t.Fatal(r.URL.String())
		}
		_, _ = w.Write([]byte(`{"openid":"wx-user"}`))
	}))
	defer server.Close()
	svc := &Service{WechatBase: server.URL, HTTP: server.Client()}
	profile, err := svc.ExchangeCode(context.Background(), SocialClient{SocialType: 32, ClientID: "app", ClientSecret: "sec"}, "auth-code", "")
	if err != nil || profile.OpenID != "wx-user" {
		t.Fatal(profile, err)
	}
}
