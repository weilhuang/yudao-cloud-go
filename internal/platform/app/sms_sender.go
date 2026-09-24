package app

import (
	"context"
	"errors"

	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
	"github.com/weilhuang/yudao-cloud-go/internal/system/message"
)

// codeSender 把登录验证码交给已有短信服务。
// 只有日志状态为发送成功时才保留验证码；模板缺失、渠道关闭或厂商失败都会让调用方作废刚写入的码。
type codeSender struct {
	send   func(ctx context.Context, mobile, template string, params map[string]any) (int64, error)
	status func(ctx context.Context, id int64) (int, error)
}

func (c codeSender) SendCode(ctx context.Context, mobile, template, code string) error {
	id, err := c.send(ctx, mobile, template, map[string]any{"code": code})
	if err != nil {
		var biz *message.Error
		if errors.As(err, &biz) {
			return &auth.Error{Code: biz.Code, Msg: biz.Msg}
		}
		return err
	}
	if c.status == nil {
		return &auth.Error{Code: 500, Msg: "系统异常"}
	}
	status, err := c.status(ctx, id)
	if err != nil {
		return err
	}
	if !message.SmsDelivered(status) {
		return &auth.Error{Code: 500, Msg: "短信发送失败"}
	}
	return nil
}
