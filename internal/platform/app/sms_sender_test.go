package app

import (
	"context"
	"testing"

	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

func TestCodeSenderRejectsUndeliveredSms(t *testing.T) {
	sender := codeSender{
		send:   func(context.Context, string, string, map[string]any) (int64, error) { return 7, nil },
		status: func(context.Context, int64) (int, error) { return 20, nil },
	}
	err := sender.SendCode(context.Background(), "15601691300", "admin-sms-login", "135790")
	biz, _ := err.(*auth.Error)
	if biz == nil || biz.Msg != "短信发送失败" {
		t.Fatalf("未送达的验证码不能保留：%v", err)
	}
	sender.status = func(context.Context, int64) (int, error) { return 10, nil }
	if err := sender.SendCode(context.Background(), "15601691300", "admin-sms-login", "135790"); err != nil {
		t.Fatal(err)
	}
}
