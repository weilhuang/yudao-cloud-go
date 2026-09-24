package directory

import "testing"

func TestOAuthUserValidationAndScope(t *testing.T) {
	nick, email, mobile := "昵称", "user@example.com", "15601691300"
	if err := validateOAuthUser(&nick, &email, &mobile); err != nil {
		t.Fatal(err)
	}
	if err := validateOAuthUser(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	long := string(make([]rune, 31))
	if err := validateOAuthUser(&long, nil, nil); err == nil || err.(*Error).Msg != "用户昵称长度不能超过 30 个字符" {
		t.Fatal(err)
	}
	bad, empty := "bad", ""
	if err := validateOAuthUser(nil, &bad, nil); err == nil || err.(*Error).Msg != "邮箱格式不正确" {
		t.Fatal(err)
	}
	if err := validateOAuthUser(nil, &empty, nil); err != nil {
		t.Fatal(err)
	}
	short := "123"
	if err := validateOAuthUser(nil, nil, &short); err == nil || err.(*Error).Msg != "手机号长度必须 11 位" {
		t.Fatal(err)
	}
	if err := validateOAuthUser(nil, nil, &empty); err == nil || err.(*Error).Msg != "手机号长度必须 11 位" {
		t.Fatal(err)
	}
	if scopeAllowed([]string{"user.write"}, "user.read") || !scopeAllowed([]string{"user.read"}, "user.read") {
		t.Fatal("scope 必须精确匹配")
	}
}
