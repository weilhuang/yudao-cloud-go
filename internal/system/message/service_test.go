package message

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendNotifyRendersAndSkipsDisabled(t *testing.T) {
	store := &memStore{notify: &NotifyTemplate{ID: 3, Code: "welcome", Content: "你好{name}", Nickname: "系统", Status: 0, Params: []string{"name"}}}
	svc := &Service{Store: store}
	id, err := svc.SendNotify(context.Background(), 1, 8, 2, "welcome", map[string]any{"name": "张三"})
	if err != nil || id != 11 || store.message.TemplateContent != "你好张三" {
		t.Fatal(id, err, store.message.TemplateContent)
	}
	store.notify.Status = 1
	id, err = svc.SendNotify(context.Background(), 1, 8, 2, "welcome", map[string]any{"name": "张三"})
	if err != nil || id != 0 {
		t.Fatal(id, err)
	}
}

func TestSendMailUsesMailer(t *testing.T) {
	store := &memStore{
		mail:    &MailTemplate{ID: 4, Code: "reset", Title: "重置{name}", Content: "内容", Status: 0, AccountID: 2, Params: []string{"name"}},
		account: &MailAccount{ID: 2, Mail: "a@b.c", Host: "smtp.example.com", Port: 465},
	}
	var subject string
	svc := &Service{Store: store, Mailer: func(_ MailAccount, _ []string, got, _ string) error {
		subject = got
		return nil
	}}
	id, err := svc.SendMail(context.Background(), []string{"u@b.c"}, "reset", map[string]any{"name": "李四"})
	if err != nil || id == 0 || subject != "重置李四" || store.mailStatus != sendSuccess {
		t.Fatal(id, err, subject, store.mailStatus)
	}
}

func TestDeleteChannelBlockedByTemplate(t *testing.T) {
	svc := &Service{Store: &memStore{channel: &SmsChannel{ID: 1}, channelTemplates: 2}}
	err := svc.DeleteChannel(context.Background(), 1)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_011_002 {
		t.Fatal(err)
	}
}

func TestDebugDingTalkSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !contains(string(body), "13800000000") {
			t.Fatal(string(body))
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()
	store := &memStore{
		sms:     &SmsTemplate{ID: 5, Code: "login", Content: "码{code}", Status: 0, ChannelID: 7, Params: []string{"code"}, APITemplateID: "T"},
		channel: &SmsChannel{ID: 7, Code: "DEBUG_DING_TALK", Status: 0, APIKey: "token", APISecret: "secret"},
	}
	svc := &Service{Store: store, Ding: server.URL, HTTP: server.Client()}
	id, err := svc.SendSms(context.Background(), "13800000000", "login", map[string]any{"code": "1234"})
	if err != nil || id == 0 || store.smsStatus != sendSuccess || store.apiCode != "0" {
		t.Fatal(id, err, store.smsStatus, store.apiCode)
	}
}

func TestVendorSMSMarksSuccess(t *testing.T) {
	cases := []struct {
		code string
		body string
		want string
	}{
		{"ALIYUN", `{"Code":"OK","Message":"OK","BizId":"biz-1"}`, "OK"},
		{"TENCENT", `{"Response":{"SendStatusSet":[{"Code":"Ok","Message":"send success","SerialNo":"s1"}]}}`, "Ok"},
		{"HUAWEI", `{"code":"000000","description":"Success","result":[{"smsMsgId":"h1","status":"000000"}]}`, "000000"},
		{"QINIU", `{"message_id":"q1"}`, ""},
	}
	for _, item := range cases {
		t.Run(item.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					t.Fatal("missing authorization")
				}
				_, _ = w.Write([]byte(item.body))
			}))
			defer server.Close()
			store := &memStore{
				sms: &SmsTemplate{ID: 5, Code: "login", Content: "码{code}", Status: 0, ChannelID: 2, Params: []string{"code"}, APITemplateID: "TPL"},
				channel: &SmsChannel{
					ID: 2, Code: item.code, Status: 0, Signature: "测试",
					APIKey: apiKey(item.code), APISecret: "secret",
				},
			}
			svc := &Service{Store: store, HTTP: server.Client()}
			switch item.code {
			case "ALIYUN":
				svc.AliyunURL = server.URL
			case "TENCENT":
				svc.TencentURL = server.URL
			case "HUAWEI":
				svc.HuaweiURL = server.URL + "/sms/batchSendSms/v1"
			case "QINIU":
				svc.QiniuURL = server.URL
			}
			id, err := svc.SendSms(context.Background(), "13800000000", "login", map[string]any{"code": "1234"})
			if err != nil || id == 0 || store.smsStatus != sendSuccess || store.apiCode != item.want {
				t.Fatal(id, err, store.smsStatus, store.apiCode)
			}
		})
	}
}

func apiKey(code string) string {
	switch code {
	case "TENCENT":
		return "secretId sdkApp"
	case "HUAWEI":
		return "accessKey sender"
	default:
		return "key"
	}
}

func contains(text, part string) bool {
	return len(text) >= len(part) && (text == part || len(part) == 0 || (func() bool {
		for i := 0; i+len(part) <= len(text); i++ {
			if text[i:i+len(part)] == part {
				return true
			}
		}
		return false
	})())
}

type memStore struct {
	notify           *NotifyTemplate
	message          NotifyMessage
	mail             *MailTemplate
	account          *MailAccount
	mailStatus       int
	channel          *SmsChannel
	channelTemplates int
	sms              *SmsTemplate
	smsStatus        int
	apiCode          string
	accountTemplates map[int64]int
	deletedAccounts  []int64
	deletedChannels  []int64
}

func (m *memStore) NotifyByID(context.Context, int64) (*NotifyTemplate, error) { return m.notify, nil }
func (m *memStore) NotifyByCode(context.Context, string) (*NotifyTemplate, error) {
	return m.notify, nil
}
func (m *memStore) NotifyCodeTaken(context.Context, string, int64) (bool, error) { return false, nil }
func (m *memStore) CreateNotify(context.Context, NotifyTemplate) (int64, error)  { return 1, nil }
func (m *memStore) UpdateNotify(context.Context, NotifyTemplate) error           { return nil }
func (m *memStore) DeleteNotify(context.Context, int64) error                    { return nil }
func (m *memStore) InsertMessage(_ context.Context, _ int64, item NotifyMessage, _ string) (int64, error) {
	m.message = item
	return 11, nil
}
func (m *memStore) AccountByID(context.Context, int64) (*MailAccount, error) { return m.account, nil }
func (m *memStore) AccountTemplateCount(_ context.Context, id int64) (int, error) {
	return m.accountTemplates[id], nil
}
func (m *memStore) CreateAccount(context.Context, MailAccount) (int64, error) {
	return 1, nil
}
func (m *memStore) UpdateAccount(context.Context, MailAccount) error { return nil }
func (m *memStore) DeleteAccount(context.Context, int64) error       { return nil }
func (m *memStore) DeleteAccountList(_ context.Context, ids []int64) error {
	m.deletedAccounts = append(m.deletedAccounts, ids...)
	return nil
}
func (m *memStore) MailByID(context.Context, int64) (*MailTemplate, error) {
	return m.mail, nil
}
func (m *memStore) MailByCode(context.Context, string) (*MailTemplate, error) { return m.mail, nil }
func (m *memStore) MailCodeTaken(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (m *memStore) CreateMail(context.Context, MailTemplate) (int64, error) { return 1, nil }
func (m *memStore) UpdateMail(context.Context, MailTemplate) error          { return nil }
func (m *memStore) DeleteMail(context.Context, int64) error                 { return nil }
func (m *memStore) InsertMailLog(context.Context, MailAccount, MailTemplate, []string, string, string, string, int) (int64, error) {
	return 21, nil
}
func (m *memStore) FinishMailLog(_ context.Context, _ int64, status int, _ string) error {
	m.mailStatus = status
	return nil
}
func (m *memStore) ChannelByID(context.Context, int64) (*SmsChannel, error) { return m.channel, nil }
func (m *memStore) ChannelTemplateCount(context.Context, int64) (int, error) {
	return m.channelTemplates, nil
}
func (m *memStore) CreateChannel(context.Context, SmsChannel) (int64, error) { return 1, nil }
func (m *memStore) UpdateChannel(context.Context, SmsChannel) error          { return nil }
func (m *memStore) DeleteChannel(context.Context, int64) error               { return nil }
func (m *memStore) DeleteChannelList(_ context.Context, ids []int64) error {
	m.deletedChannels = append(m.deletedChannels, ids...)
	return nil
}
func (m *memStore) SmsByID(context.Context, int64) (*SmsTemplate, error)    { return m.sms, nil }
func (m *memStore) SmsByCode(context.Context, string) (*SmsTemplate, error) { return m.sms, nil }
func (m *memStore) SmsCodeTaken(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (m *memStore) CreateSms(context.Context, SmsTemplate) (int64, error) { return 1, nil }
func (m *memStore) UpdateSms(context.Context, SmsTemplate) error          { return nil }
func (m *memStore) DeleteSms(context.Context, int64) error                { return nil }
func (m *memStore) InsertSmsLog(context.Context, SmsChannel, SmsTemplate, string, string, string, int, int64, int) (int64, error) {
	return 31, nil
}
func (m *memStore) FinishSmsLog(_ context.Context, _ int64, status int, apiCode, _, _ string) error {
	m.smsStatus = status
	m.apiCode = apiCode
	return nil
}

func TestDeleteAccountListStopsWhenTemplateRemains(t *testing.T) {
	store := &memStore{accountTemplates: map[int64]int{2: 1}}
	svc := &Service{Store: store}
	err := svc.DeleteAccountList(context.Background(), []int64{1, 2})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_023_001 || len(store.deletedAccounts) != 0 {
		t.Fatalf("有模板时不应删除：%v %v", err, store.deletedAccounts)
	}
	if err := svc.DeleteAccountList(context.Background(), []int64{1, 3}); err != nil {
		t.Fatal(err)
	}
	if len(store.deletedAccounts) != 2 || store.deletedAccounts[0] != 1 || store.deletedAccounts[1] != 3 {
		t.Fatalf("无模板时应一次删除：%v", store.deletedAccounts)
	}
}

func TestDeleteAccountListEvictsMailAccountCache(t *testing.T) {
	store := &memStore{}
	var names []string
	svc := &Service{Store: store, AfterCommit: func(_ context.Context, name string) error {
		names = append(names, name)
		return nil
	}}
	if err := svc.DeleteAccountList(context.Background(), []int64{1}); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "mail_account" {
		t.Fatalf("删除成功后应清空 mail_account：%v", names)
	}
	svc.AfterCommit = func(context.Context, string) error { return context.DeadlineExceeded }
	err := svc.DeleteAccountList(context.Background(), []int64{4})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 500 {
		t.Fatalf("缓存失败应说明数据库已提交：%v", err)
	}
}
