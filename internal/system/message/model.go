// Package message 提供站内信、邮件和短信。发送在进程内完成，不经过消息队列。
package message

// NotifyTemplate 是站内信模板。
type NotifyTemplate struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Code       string   `json:"code"`
	Nickname   string   `json:"nickname"`
	Content    string   `json:"content"`
	Type       int      `json:"type"`
	Params     []string `json:"params"`
	Status     int      `json:"status"`
	Remark     string   `json:"remark"`
	CreateTime int64    `json:"createTime,omitempty"`
}

// NotifyMessage 是一条站内信。
type NotifyMessage struct {
	ID               int64  `json:"id"`
	UserID           int64  `json:"userId"`
	UserType         int    `json:"userType"`
	TemplateID       int64  `json:"templateId"`
	TemplateCode     string `json:"templateCode"`
	TemplateNickname string `json:"templateNickname"`
	TemplateContent  string `json:"templateContent"`
	TemplateType     int    `json:"templateType"`
	ReadStatus       bool   `json:"readStatus"`
	ReadTime         *int64 `json:"readTime"`
	CreateTime       int64  `json:"createTime"`
}

// MailAccount 是发信账号。
type MailAccount struct {
	ID             int64  `json:"id"`
	Mail           string `json:"mail"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	SSLEnable      bool   `json:"sslEnable"`
	StartTLSEnable bool   `json:"starttlsEnable"`
	CreateTime     int64  `json:"createTime,omitempty"`
}

// MailTemplate 是邮件模板。
type MailTemplate struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Code       string   `json:"code"`
	AccountID  int64    `json:"accountId"`
	Nickname   string   `json:"nickname"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Params     []string `json:"params"`
	Status     int      `json:"status"`
	Remark     string   `json:"remark"`
	CreateTime int64    `json:"createTime,omitempty"`
}

// SmsChannel 是短信渠道。Code 为 DEBUG_DING_TALK、ALIYUN、TENCENT、HUAWEI、QINIU。
type SmsChannel struct {
	ID          int64  `json:"id"`
	Signature   string `json:"signature"`
	Code        string `json:"code"`
	Status      int    `json:"status"`
	Remark      string `json:"remark"`
	APIKey      string `json:"apiKey"`
	APISecret   string `json:"apiSecret"`
	CallbackURL string `json:"callbackUrl"`
	CreateTime  int64  `json:"createTime,omitempty"`
}

// SmsTemplate 是短信模板。
type SmsTemplate struct {
	ID            int64    `json:"id"`
	Type          int      `json:"type"`
	Status        int      `json:"status"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	Content       string   `json:"content"`
	Params        []string `json:"params"`
	Remark        string   `json:"remark"`
	APITemplateID string   `json:"apiTemplateId"`
	ChannelID     int64    `json:"channelId"`
	ChannelCode   string   `json:"channelCode"`
	CreateTime    int64    `json:"createTime,omitempty"`
}

// Page 是管理后台分页。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// Error 是消息接口的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

const (
	sendInit    = 0
	sendSuccess = 10
	sendFailure = 20
	userAdmin   = 2
)

// SmsDelivered 表示渠道已经接受这条短信。登录验证码只有送达后才保留。
func SmsDelivered(status int) bool { return status == sendSuccess }
