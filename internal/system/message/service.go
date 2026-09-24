package message

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Store 读写站内信、邮件和短信。
type Store interface {
	NotifyByID(ctx context.Context, id int64) (*NotifyTemplate, error)
	NotifyByCode(ctx context.Context, code string) (*NotifyTemplate, error)
	NotifyCodeTaken(ctx context.Context, code string, exceptID int64) (bool, error)
	CreateNotify(ctx context.Context, item NotifyTemplate) (int64, error)
	UpdateNotify(ctx context.Context, item NotifyTemplate) error
	DeleteNotify(ctx context.Context, id int64) error
	InsertMessage(ctx context.Context, tenantID int64, item NotifyMessage, params string) (int64, error)

	AccountByID(ctx context.Context, id int64) (*MailAccount, error)
	AccountTemplateCount(ctx context.Context, id int64) (int, error)
	CreateAccount(ctx context.Context, item MailAccount) (int64, error)
	UpdateAccount(ctx context.Context, item MailAccount) error
	DeleteAccount(ctx context.Context, id int64) error
	DeleteAccountList(ctx context.Context, ids []int64) error
	MailByID(ctx context.Context, id int64) (*MailTemplate, error)
	MailByCode(ctx context.Context, code string) (*MailTemplate, error)
	MailCodeTaken(ctx context.Context, code string, exceptID int64) (bool, error)
	CreateMail(ctx context.Context, item MailTemplate) (int64, error)
	UpdateMail(ctx context.Context, item MailTemplate) error
	DeleteMail(ctx context.Context, id int64) error
	InsertMailLog(ctx context.Context, account MailAccount, template MailTemplate, to []string, title, content, params string, status int) (int64, error)
	FinishMailLog(ctx context.Context, id int64, status int, errText string) error

	ChannelByID(ctx context.Context, id int64) (*SmsChannel, error)
	ChannelTemplateCount(ctx context.Context, id int64) (int, error)
	CreateChannel(ctx context.Context, item SmsChannel) (int64, error)
	UpdateChannel(ctx context.Context, item SmsChannel) error
	DeleteChannel(ctx context.Context, id int64) error
	DeleteChannelList(ctx context.Context, ids []int64) error
	SmsByID(ctx context.Context, id int64) (*SmsTemplate, error)
	SmsByCode(ctx context.Context, code string) (*SmsTemplate, error)
	SmsCodeTaken(ctx context.Context, code string, exceptID int64) (bool, error)
	CreateSms(ctx context.Context, item SmsTemplate) (int64, error)
	UpdateSms(ctx context.Context, item SmsTemplate) error
	DeleteSms(ctx context.Context, id int64) error
	InsertSmsLog(ctx context.Context, channel SmsChannel, template SmsTemplate, mobile, content, params string, status int, userID int64, userType int) (int64, error)
	FinishSmsLog(ctx context.Context, id int64, status int, apiCode, apiMsg, serial string) error
}

// Service 发送站内信、邮件和短信。Mailer 为空时走 SMTP，HTTP 用来打钉钉调试机器人。
type Service struct {
	Store      Store
	Mailer     func(account MailAccount, to []string, subject, body string) error
	HTTP       *http.Client
	Ding       string
	AliyunURL  string
	TencentURL string
	HuaweiURL  string
	QiniuURL   string
	// AfterCommit 在批量删除已写入后清空 Java 缓存。为空表示当前进程不与 Java 混跑。
	AfterCommit func(ctx context.Context, name string) error
}

func (s *Service) evictCommitted(ctx context.Context, name string) error {
	if s.AfterCommit == nil {
		return nil
	}
	if err := s.AfterCommit(ctx, name); err != nil {
		return &Error{Code: 500, Msg: "数据库已提交，但 Java 缓存失效失败；请检查 Redis 并补偿清理缓存"}
	}
	return nil
}

// SaveNotify 创建或修改站内信模板，参数从正文里的 {变量} 解析。
func (s *Service) SaveNotify(ctx context.Context, item NotifyTemplate) (int64, error) {
	if item.Name == "" || item.Code == "" || item.Content == "" {
		return 0, &Error{Code: 400, Msg: "模板名称、编码和内容不能为空"}
	}
	taken, err := s.Store.NotifyCodeTaken(ctx, item.Code, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_026_001, Msg: fmt.Sprintf("已经存在编码为【%s】的站内信模板", item.Code)}
	}
	item.Params = parseParams(item.Content)
	if item.ID == 0 {
		return s.Store.CreateNotify(ctx, item)
	}
	current, err := s.Store.NotifyByID(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_002_026_000, Msg: "站内信模版不存在"}
	}
	return item.ID, s.Store.UpdateNotify(ctx, item)
}

// SendNotify 把渲染后的站内信写给指定用户。模板关闭时不发送，返回 0。
func (s *Service) SendNotify(ctx context.Context, tenantID, userID int64, userType int, code string, params map[string]any) (int64, error) {
	template, err := s.Store.NotifyByCode(ctx, code)
	if err != nil {
		return 0, err
	}
	if template == nil {
		return 0, &Error{Code: 1_002_008_001, Msg: "当前通知公告不存在"}
	}
	if miss := missingParam(template.Params, params); miss != "" {
		return 0, &Error{Code: 1_002_028_000, Msg: fmt.Sprintf("模板参数(%s)缺失", miss)}
	}
	if template.Status != 0 {
		return 0, nil
	}
	if userType == 0 {
		userType = userAdmin
	}
	content := formatContent(template.Content, params)
	raw, _ := json.Marshal(params)
	return s.Store.InsertMessage(ctx, tenantID, NotifyMessage{
		UserID: userID, UserType: userType, TemplateID: template.ID, TemplateCode: template.Code,
		TemplateNickname: template.Nickname, TemplateContent: content, TemplateType: template.Type,
	}, string(raw))
}

// DeleteAccount 仍被模板使用时不能删。
func (s *Service) DeleteAccount(ctx context.Context, id int64) error {
	current, err := s.Store.AccountByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_023_000, Msg: "邮箱账号不存在"}
	}
	count, err := s.Store.AccountTemplateCount(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return &Error{Code: 1_002_023_001, Msg: "无法删除，该邮箱账号还有邮件模板"}
	}
	return s.Store.DeleteAccount(ctx, id)
}

// DeleteAccountList 先确认每个账号都没有邮件模板，再一次性删除。任一账号仍被模板使用时整批不删。
func (s *Service) DeleteAccountList(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		count, err := s.Store.AccountTemplateCount(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return &Error{Code: 1_002_023_001, Msg: "无法删除，该邮箱账号还有邮件模板"}
		}
	}
	if err := s.Store.DeleteAccountList(ctx, ids); err != nil {
		return err
	}
	return s.evictCommitted(ctx, "mail_account")
}

// SaveMail 创建或修改邮件模板。
func (s *Service) SaveMail(ctx context.Context, item MailTemplate) (int64, error) {
	if item.Name == "" || item.Code == "" || item.Title == "" {
		return 0, &Error{Code: 400, Msg: "模板名称、编码和标题不能为空"}
	}
	account, err := s.Store.AccountByID(ctx, item.AccountID)
	if err != nil {
		return 0, err
	}
	if account == nil {
		return 0, &Error{Code: 1_002_023_000, Msg: "邮箱账号不存在"}
	}
	taken, err := s.Store.MailCodeTaken(ctx, item.Code, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_024_001, Msg: fmt.Sprintf("邮件模版 code(%s) 已存在", item.Code)}
	}
	item.Params = parseParams(item.Content + item.Title)
	if item.ID == 0 {
		return s.Store.CreateMail(ctx, item)
	}
	current, err := s.Store.MailByID(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_002_024_000, Msg: "邮件模版不存在"}
	}
	return item.ID, s.Store.UpdateMail(ctx, item)
}

// SendMail 渲染后通过 SMTP 发出。模板或账号关闭时只记日志。
func (s *Service) SendMail(ctx context.Context, to []string, code string, params map[string]any) (int64, error) {
	if len(to) == 0 {
		return 0, &Error{Code: 1_002_025_001, Msg: "邮箱不存在"}
	}
	template, err := s.Store.MailByCode(ctx, code)
	if err != nil {
		return 0, err
	}
	if template == nil {
		return 0, &Error{Code: 1_002_024_000, Msg: "邮件模版不存在"}
	}
	if miss := missingParam(template.Params, params); miss != "" {
		return 0, &Error{Code: 1_002_025_000, Msg: fmt.Sprintf("模板参数(%s)缺失", miss)}
	}
	account, err := s.Store.AccountByID(ctx, template.AccountID)
	if err != nil {
		return 0, err
	}
	if account == nil {
		return 0, &Error{Code: 1_002_023_000, Msg: "邮箱账号不存在"}
	}
	title := formatContent(template.Title, params)
	content := formatContent(template.Content, params)
	raw, _ := json.Marshal(params)
	status := sendInit
	if template.Status != 0 {
		status = sendFailure
	}
	logID, err := s.Store.InsertMailLog(ctx, *account, *template, to, title, content, string(raw), status)
	if err != nil || template.Status != 0 {
		return logID, err
	}
	send := s.Mailer
	if send == nil {
		send = smtpSend
	}
	if err := send(*account, to, title, content); err != nil {
		_ = s.Store.FinishMailLog(ctx, logID, sendFailure, err.Error())
		return logID, nil
	}
	return logID, s.Store.FinishMailLog(ctx, logID, sendSuccess, "")
}

// DeleteChannel 还有模板时不能删。
func (s *Service) DeleteChannel(ctx context.Context, id int64) error {
	current, err := s.Store.ChannelByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_011_000, Msg: "短信渠道不存在"}
	}
	count, err := s.Store.ChannelTemplateCount(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return &Error{Code: 1_002_011_002, Msg: "无法删除，该短信渠道还有短信模板"}
	}
	return s.Store.DeleteChannel(ctx, id)
}

// DeleteChannelList 先确认每个渠道都没有短信模板，再一次性删除。
func (s *Service) DeleteChannelList(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		count, err := s.Store.ChannelTemplateCount(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return &Error{Code: 1_002_011_002, Msg: "无法删除，该短信渠道还有短信模板"}
		}
	}
	return s.Store.DeleteChannelList(ctx, ids)
}

// SaveSms 创建或修改短信模板。渠道必须存在且开启。
func (s *Service) SaveSms(ctx context.Context, item SmsTemplate) (int64, error) {
	if item.Code == "" || item.Name == "" || item.Content == "" {
		return 0, &Error{Code: 400, Msg: "模板编码、名称和内容不能为空"}
	}
	channel, err := s.Store.ChannelByID(ctx, item.ChannelID)
	if err != nil {
		return 0, err
	}
	if channel == nil {
		return 0, &Error{Code: 1_002_011_000, Msg: "短信渠道不存在"}
	}
	if channel.Status != 0 {
		return 0, &Error{Code: 1_002_011_001, Msg: "短信渠道不处于开启状态，不允许选择"}
	}
	taken, err := s.Store.SmsCodeTaken(ctx, item.Code, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_012_001, Msg: fmt.Sprintf("已经存在编码为【%s】的短信模板", item.Code)}
	}
	item.ChannelCode = channel.Code
	item.Params = parseParams(item.Content)
	if item.ID == 0 {
		return s.Store.CreateSms(ctx, item)
	}
	current, err := s.Store.SmsByID(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_002_012_000, Msg: "短信模板不存在"}
	}
	return item.ID, s.Store.UpdateSms(ctx, item)
}

// SendSms 校验模板和渠道后发送。调试渠道打钉钉机器人，云厂商走各自的 HTTP 接口。
func (s *Service) SendSms(ctx context.Context, mobile, code string, params map[string]any) (int64, error) {
	return s.SendSmsTo(ctx, mobile, code, params, 0, userAdmin)
}

// SendSmsTo 与 SendSms 相同，但把用户编号和用户类型写入短信日志。Feign 发给管理员或会员时使用。
func (s *Service) SendSmsTo(ctx context.Context, mobile, code string, params map[string]any, userID int64, userType int) (int64, error) {
	if mobile == "" {
		return 0, &Error{Code: 1_002_013_000, Msg: "手机号不存在"}
	}
	template, err := s.Store.SmsByCode(ctx, code)
	if err != nil {
		return 0, err
	}
	if template == nil {
		return 0, &Error{Code: 1_002_013_002, Msg: "短信模板不存在"}
	}
	channel, err := s.Store.ChannelByID(ctx, template.ChannelID)
	if err != nil {
		return 0, err
	}
	if channel == nil {
		return 0, &Error{Code: 1_002_011_000, Msg: "短信渠道不存在"}
	}
	if miss := missingParam(template.Params, params); miss != "" {
		return 0, &Error{Code: 1_002_013_001, Msg: fmt.Sprintf("模板参数(%s)缺失", miss)}
	}
	content := formatContent(template.Content, params)
	raw, _ := json.Marshal(params)
	enabled := template.Status == 0 && channel.Status == 0
	status := sendInit
	if !enabled {
		status = sendFailure
	}
	logID, err := s.Store.InsertSmsLog(ctx, *channel, *template, mobile, content, string(raw), status, userID, userType)
	if err != nil || !enabled {
		return logID, err
	}
	var apiCode, apiMsg, serial string
	var ok bool
	if channel.Code == "DEBUG_DING_TALK" {
		apiCode, apiMsg, serial, ok, err = dingTalkSend(ctx, s.HTTP, s.Ding, channel.APIKey, channel.APISecret, mobile, logID, params)
	} else {
		var result smsResult
		result, err = sendVendorSMS(ctx, s.HTTP, *channel, *template, mobile, logID, params, smsBases{
			aliyun: s.AliyunURL, tencent: s.TencentURL, huawei: s.HuaweiURL, qiniu: s.QiniuURL,
		})
		apiCode, apiMsg, serial, ok = result.code, result.msg, result.serial, result.ok
	}
	if err != nil {
		_ = s.Store.FinishSmsLog(ctx, logID, sendFailure, "EXCEPTION", err.Error(), "")
		return logID, nil
	}
	next := sendFailure
	if ok {
		next = sendSuccess
	}
	return logID, s.Store.FinishSmsLog(ctx, logID, next, apiCode, apiMsg, serial)
}
