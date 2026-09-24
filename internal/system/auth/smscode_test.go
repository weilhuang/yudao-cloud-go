package auth

import (
	"context"
	"testing"
	"time"
)

type memSms struct {
	items []SmsCode
	next  int64
}

func (m *memSms) LastSmsCode(_ context.Context, mobile, code string, scene int, filterCode, filterScene bool) (*SmsCode, error) {
	for i := len(m.items) - 1; i >= 0; i-- {
		item := m.items[i]
		if item.Mobile != mobile {
			continue
		}
		if filterScene && item.Scene != scene {
			continue
		}
		if filterCode && item.Code != code {
			continue
		}
		copy := item
		return &copy, nil
	}
	return nil, nil
}

func (m *memSms) InsertSmsCode(_ context.Context, item SmsCode, _ string) error {
	m.next++
	item.ID = m.next
	m.items = append(m.items, item)
	return nil
}

func (m *memSms) UseSmsCode(_ context.Context, id int64, _ string, _ time.Time) error {
	for i := range m.items {
		if m.items[i].ID == id {
			if m.items[i].Used {
				return &Error{Code: codeSmsUsed, Msg: "验证码已使用"}
			}
			m.items[i].Used = true
			return nil
		}
	}
	return &Error{Code: codeSmsUsed, Msg: "验证码已使用"}
}

func (m *memSms) DiscardSmsCode(_ context.Context, mobile, code string, scene int) error {
	for i := len(m.items) - 1; i >= 0; i-- {
		item := m.items[i]
		if item.Mobile == mobile && item.Code == code && item.Scene == scene && !item.Used {
			m.items = append(m.items[:i], m.items[i+1:]...)
			return nil
		}
	}
	return nil
}

type recordingSender struct {
	last string
}

func (r *recordingSender) SendCode(_ context.Context, _, _, code string) error {
	r.last = code
	return nil
}

type smsUsers struct {
	memUsers
	byMobile map[string]*User
	enabled  string
	count    int64
	limit    int64
	created  *User
	password string
}

func (s *smsUsers) FindByMobile(_ context.Context, _ int64, mobile string) (*User, error) {
	return s.byMobile[mobile], nil
}
func (s *smsUsers) ConfigValue(context.Context, string) (string, error) { return s.enabled, nil }
func (s *smsUsers) CountUsers(context.Context, int64) (int64, error)    { return s.count, nil }
func (s *smsUsers) AccountLimit(context.Context, int64) (int64, error)  { return s.limit, nil }
func (s *smsUsers) FindByUsername(_ context.Context, _ int64, username string) (*User, error) {
	if s.created != nil && s.created.Username == username {
		return s.created, nil
	}
	return nil, nil
}
func (s *smsUsers) RegisterUser(_ context.Context, tenantID int64, username, nickname, hash string) (*User, error) {
	s.created = &User{ID: 8, TenantID: tenantID, Username: username, Nickname: nickname, Password: hash, Status: statusEnable}
	s.count++
	return s.created, nil
}
func (s *smsUsers) UpdatePassword(_ context.Context, _, _ int64, hash string) error {
	s.password = hash
	return nil
}

func TestSendSmsCodeStoresFixedTestCodeWithoutVendor(t *testing.T) {
	sms := &memSms{}
	sender := &recordingSender{}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, shanghai)
	user := &User{ID: 7, TenantID: 1, Username: "admin", Status: statusEnable}
	svc := &Service{
		Users: &smsUsers{byMobile: map[string]*User{"15601691300": user}},
		Sms:   sms, Codes: sender, Now: func() time.Time { return now },
		CodeBegin: 9999, CodeEnd: 9999,
	}
	if err := svc.SendSmsCode(context.Background(), 1, "15601691300", sceneLogin, "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if sender.last != "9999" || len(sms.items) != 1 || sms.items[0].Code != "9999" {
		t.Fatalf("应只在内存记录固定测试验证码：sender=%s items=%+v", sender.last, sms.items)
	}
	err := svc.SendSmsCode(context.Background(), 1, "15601691300", sceneLogin, "", "127.0.0.1")
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != codeSmsFast {
		t.Fatal(err)
	}
	if err := svc.SendSmsCode(context.Background(), 1, "15600000000", sceneLogin, "", "127.0.0.1"); err == nil || err.(*Error).Code != codeMobileMissing {
		t.Fatal(err)
	}
}

func TestSmsLoginAndResetPasswordConsumeCode(t *testing.T) {
	sms := &memSms{}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, shanghai)
	user := &User{ID: 7, TenantID: 1, Username: "admin", Status: statusEnable}
	users := &smsUsers{byMobile: map[string]*User{"15601691300": user}}
	svc := &Service{
		Users: users, Sms: sms, Tokens: &memTokens{client: &Client{ClientID: clientIDDefault, AccessTTL: time.Hour, RefreshTTL: time.Hour}},
		Cache: &memCache{items: map[string]Token{}}, Now: func() time.Time { return now },
		CodeBegin: 9999, CodeEnd: 9999,
	}
	if err := svc.SendSmsCode(context.Background(), 1, "15601691300", sceneLogin, "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	result, err := svc.SmsLogin(context.Background(), 1, "15601691300", "9999", "127.0.0.1", RequestMeta{IP: "127.0.0.1"})
	if err != nil || result.UserID != 7 || result.AccessToken == "" {
		t.Fatal(err, result)
	}
	if _, err := svc.SmsLogin(context.Background(), 1, "15601691300", "9999", "127.0.0.1", RequestMeta{}); err == nil || err.(*Error).Code != codeSmsUsed {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := svc.SendSmsCode(context.Background(), 1, "15601691300", sceneReset, "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(context.Background(), 1, "15601691300", "9999", "newpass", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if users.password == "" {
		t.Fatal("密码未更新")
	}
}

func TestRegisterRequiresSwitchAndQuota(t *testing.T) {
	users := &smsUsers{enabled: "false", limit: 10}
	svc := &Service{Users: users, Tokens: &memTokens{client: &Client{ClientID: clientIDDefault, AccessTTL: time.Hour, RefreshTTL: time.Hour}}, Cache: &memCache{items: map[string]Token{}}}
	_, err := svc.Register(context.Background(), 1, "newuser", "新用户", "admin123", "", RequestMeta{})
	if err == nil || err.(*Error).Code != codeRegisterOff {
		t.Fatal(err)
	}
	users.enabled = "true"
	users.limit = 0
	_, err = svc.Register(context.Background(), 1, "newuser", "新用户", "admin123", "", RequestMeta{})
	if err == nil || err.(*Error).Code != codeUserCount {
		t.Fatal(err)
	}
	users.limit = 10
	result, err := svc.Register(context.Background(), 1, "newuser", "新用户", "admin123", "", RequestMeta{})
	if err != nil || result.UserID != 8 {
		t.Fatal(err, result)
	}
}

func TestDefaultSmsCodeIsSixDigitsNot9999(t *testing.T) {
	sms := &memSms{}
	svc := &Service{Users: &smsUsers{byMobile: map[string]*User{"15601691300": {ID: 1, Status: statusEnable}}}, Sms: sms}
	if err := svc.SendSmsCode(context.Background(), 1, "15601691300", sceneLogin, "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if len(sms.items) != 1 || len(sms.items[0].Code) != 6 || sms.items[0].Code == "9999" {
		t.Fatalf("未配置范围时不能使用 9999：%+v", sms.items)
	}
}

type failSender struct{}

func (failSender) SendCode(context.Context, string, string, string) error {
	return &Error{Code: 1_002_013_002, Msg: "短信模板不存在"}
}

func TestFailedSmsSendDiscardsCode(t *testing.T) {
	sms := &memSms{}
	svc := &Service{
		Users: &smsUsers{byMobile: map[string]*User{"15601691300": {ID: 1, Status: statusEnable}}},
		Sms:   sms, Codes: failSender{}, CodeBegin: 9999, CodeEnd: 9999,
	}
	err := svc.SendSmsCode(context.Background(), 1, "15601691300", sceneLogin, "", "127.0.0.1")
	if err == nil || err.(*Error).Msg != "短信模板不存在" || len(sms.items) != 0 {
		t.Fatalf("发送失败后验证码仍可用于登录：%v %+v", err, sms.items)
	}
}
