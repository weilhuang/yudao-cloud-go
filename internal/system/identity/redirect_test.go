package identity

import "testing"

func TestAuthorizeURLWechatOpen(t *testing.T) {
	got, err := AuthorizeURL(32, "wx123", "https://app.example.com/callback", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" || !contains(got, "appid=wx123") || !contains(got, "snsapi_login") || !contains(got, "#wechat_redirect") {
		t.Fatal(got)
	}
}

func TestAuthorizeURLRejectsMiniProgram(t *testing.T) {
	_, err := AuthorizeURL(34, "wx", "https://app.example.com", "s")
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 400 {
		t.Fatal(err)
	}
}

func contains(text, part string) bool {
	return len(text) >= len(part) && (func() bool {
		for i := 0; i+len(part) <= len(text); i++ {
			if text[i:i+len(part)] == part {
				return true
			}
		}
		return false
	})()
}
