package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var mobilePattern = regexp.MustCompile(`^(?:(?:\+|00)86)?1(?:(?:3[\d])|(?:4[0,1,4-9])|(?:5[0-3,5-9])|(?:6[2,5-7])|(?:7[0-8])|(?:8[\d])|(?:9[0-3,5-9]))\d{8}$`)

const (
	sceneLogin    = 21
	sceneRegister = 22
	sceneReset    = 23
	logTypeMobile = 103

	codeMobileMissing   = 1_002_000_007
	codeRegisterCaptcha = 1_002_000_008
	codeSmsNotFound     = 1_002_014_000
	codeSmsExpired      = 1_002_014_001
	codeSmsUsed         = 1_002_014_002
	codeSmsDaily        = 1_002_014_004
	codeSmsFast         = 1_002_014_005
	codeUserCount       = 1_002_003_008
	codeRegisterOff     = 1_002_003_011
	codeMobileUnreg     = 1_002_003_010
	codeUsernameTaken   = 1_002_003_000
	codeUserNotExists   = 1_002_003_003
)

// SmsCode 是 system_sms_code 的一行。查询按手机号全局进行，因为 Java 标了 @TenantIgnore。
type SmsCode struct {
	ID         int64
	Mobile     string
	Code       string
	Scene      int
	TodayIndex int
	CreateTime time.Time
	Used       bool
	TenantID   int64
}

// SmsCodeStore 保存验证码。发送频率只看该手机号最近一条，不区分场景。
type SmsCodeStore interface {
	LastSmsCode(ctx context.Context, mobile string, code string, scene int, filterCode, filterScene bool) (*SmsCode, error)
	InsertSmsCode(ctx context.Context, item SmsCode, ip string) error
	UseSmsCode(ctx context.Context, id int64, ip string, at time.Time) error
	// DiscardSmsCode 删除尚未使用的验证码。发送失败时调用，避免未送达的码仍能登录。
	DiscardSmsCode(ctx context.Context, mobile, code string, scene int) error
}

// CodeSender 把验证码交给短信渠道。测试必须使用只记录、不访问外网的实现。
type CodeSender interface {
	SendCode(ctx context.Context, mobile, template, code string) error
}

func (s *Service) smsExpire() time.Duration {
	if s.SmsExpire > 0 {
		return s.SmsExpire
	}
	return 10 * time.Minute
}

func (s *Service) smsFrequency() time.Duration {
	if s.SmsFrequency > 0 {
		return s.SmsFrequency
	}
	return time.Minute
}

func (s *Service) smsDailyMax() int {
	if s.SmsDailyMax > 0 {
		return s.SmsDailyMax
	}
	return 10
}

func (s *Service) codeBounds() (int, int) {
	begin, end := s.CodeBegin, s.CodeEnd
	// 未配置时使用 6 位随机范围。9999 只在部署显式写明时生效，不能当线上默认值。
	if begin == 0 && end == 0 {
		return 100000, 999999
	}
	if end < begin {
		return 100000, 999999
	}
	return begin, end
}

// SendSmsCode 对齐 AdminAuthServiceImpl.sendSmsCode 和 SmsCodeServiceImpl。
// 管理端在写入验证码前要求手机号已经注册。重置密码场景额外校验图形验证码。
func (s *Service) SendSmsCode(ctx context.Context, tenantID int64, mobile string, scene int, captcha, ip string) error {
	if s.Sms == nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	if strings.TrimSpace(mobile) == "" {
		return badRequest("手机号不能为空")
	}
	if !mobileOK(mobile) {
		return badRequest("手机号格式不正确")
	}
	if scene != sceneLogin && scene != sceneRegister && scene != sceneReset {
		return badRequest("请求参数不正确")
	}
	if scene == sceneReset && s.CaptchaOn && (captcha == "" || (s.Captcha != nil && s.Captcha.Verify(ctx, captcha) != nil)) {
		return &Error{Code: codeRegisterCaptcha, Msg: "验证码不正确，原因：验证码不能为空"}
	}
	user, err := s.Users.FindByMobile(ctx, tenantID, mobile)
	if err != nil {
		return err
	}
	if user == nil {
		return &Error{Code: codeMobileMissing, Msg: "手机号不存在"}
	}
	code, err := s.createSmsCode(ctx, tenantID, mobile, scene, ip)
	if err != nil {
		return err
	}
	if s.Codes == nil {
		return nil
	}
	template := "admin-sms-login"
	if scene == sceneReset {
		template = "admin-reset-password"
	} else if scene == sceneRegister {
		template = "admin-sms-register"
	}
	if err := s.Codes.SendCode(ctx, mobile, template, code); err != nil {
		// 发送失败的验证码不能继续用于登录，否则未送达的码仍可被猜中。
		if delErr := s.Sms.DiscardSmsCode(ctx, mobile, code, scene); delErr != nil {
			return &Error{Code: 500, Msg: "系统异常"}
		}
		return err
	}
	return nil
}

func (s *Service) createSmsCode(ctx context.Context, tenantID int64, mobile string, scene int, ip string) (string, error) {
	last, err := s.Sms.LastSmsCode(ctx, mobile, "", 0, false, false)
	if err != nil {
		return "", err
	}
	now := s.now()
	todayIndex := 1
	if last != nil {
		if now.Sub(last.CreateTime) < s.smsFrequency() {
			return "", &Error{Code: codeSmsFast, Msg: "短信发送过于频繁"}
		}
		if sameShanghaiDay(last.CreateTime, now) {
			if last.TodayIndex >= s.smsDailyMax() {
				return "", &Error{Code: codeSmsDaily, Msg: "超过每日短信发送数量"}
			}
			todayIndex = last.TodayIndex + 1
		}
	}
	code, err := s.randomCode()
	if err != nil {
		return "", err
	}
	if err := s.Sms.InsertSmsCode(ctx, SmsCode{
		Mobile: mobile, Code: code, Scene: scene, TodayIndex: todayIndex, CreateTime: now, TenantID: tenantID,
	}, ip); err != nil {
		return "", err
	}
	return code, nil
}

func (s *Service) randomCode() (string, error) {
	begin, end := s.codeBounds()
	if end < begin {
		return "", fmt.Errorf("验证码范围不正确")
	}
	span := end - begin + 1
	n := begin
	if span > 1 {
		value, err := rand.Int(rand.Reader, bigInt(span))
		if err != nil {
			return "", err
		}
		n = begin + int(value.Int64())
	}
	width := len(strconv.Itoa(end))
	return fmt.Sprintf("%0*d", width, n), nil
}

func bigInt(n int) *big.Int { return big.NewInt(int64(n)) }

// SmsLogin 校验场景 21 的验证码后签发管理员令牌。验证码先标记已使用。
func (s *Service) SmsLogin(ctx context.Context, tenantID int64, mobile, code, ip string, meta RequestMeta) (*LoginResult, error) {
	if strings.TrimSpace(mobile) == "" {
		return nil, badRequest("手机号不能为空")
	}
	if !mobileOK(mobile) {
		return nil, badRequest("手机号格式不正确")
	}
	if strings.TrimSpace(code) == "" {
		return nil, badRequest("验证码不能为空")
	}
	if err := s.useSmsCode(ctx, mobile, code, sceneLogin, ip); err != nil {
		return nil, err
	}
	user, err := s.Users.FindByMobile(ctx, tenantID, mobile)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	if user.Status != statusEnable {
		s.recordLogin(ctx, tenantID, user.ID, mobile, logTypeMobile, 20, meta)
		return nil, userDisabled()
	}
	if err := s.checkTenant(ctx, user.TenantID); err != nil {
		return nil, err
	}
	if err := s.Users.TouchLogin(ctx, user.ID, meta.IP, s.now().UnixMilli()); err != nil {
		return nil, err
	}
	s.recordLogin(ctx, user.TenantID, user.ID, mobile, logTypeMobile, 0, meta)
	return s.issue(ctx, user)
}

func (s *Service) useSmsCode(ctx context.Context, mobile, code string, scene int, ip string) error {
	if s.Sms == nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	last, err := s.Sms.LastSmsCode(ctx, mobile, code, scene, true, true)
	if err != nil {
		return err
	}
	if last == nil {
		return &Error{Code: codeSmsNotFound, Msg: "验证码不存在"}
	}
	if s.now().Sub(last.CreateTime) >= s.smsExpire() {
		return &Error{Code: codeSmsExpired, Msg: "验证码已过期"}
	}
	if last.Used {
		return &Error{Code: codeSmsUsed, Msg: "验证码已使用"}
	}
	return s.Sms.UseSmsCode(ctx, last.ID, ip, s.now())
}

// SendSmsCodeRPC 给 Feign 创建验证码。不要求手机号已经注册，场景覆盖会员和后台。
func (s *Service) SendSmsCodeRPC(ctx context.Context, tenantID int64, mobile string, scene int, ip string) error {
	if s.Sms == nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	if strings.TrimSpace(mobile) == "" {
		return badRequest("手机号不能为空")
	}
	if !mobileOK(mobile) {
		return badRequest("手机号格式不正确")
	}
	if strings.TrimSpace(ip) == "" {
		return badRequest("发送 IP 不能为空")
	}
	template, ok := smsSceneTemplate(scene)
	if !ok {
		return &Error{Code: 500, Msg: fmt.Sprintf("验证码场景(%d) 查找不到配置", scene)}
	}
	code, err := s.createSmsCode(ctx, tenantID, mobile, scene, ip)
	if err != nil {
		return err
	}
	if s.Codes == nil {
		_ = s.Sms.DiscardSmsCode(ctx, mobile, code, scene)
		return &Error{Code: 500, Msg: "系统异常"}
	}
	if err := s.Codes.SendCode(ctx, mobile, template, code); err != nil {
		if delErr := s.Sms.DiscardSmsCode(ctx, mobile, code, scene); delErr != nil {
			return &Error{Code: 500, Msg: "系统异常"}
		}
		return err
	}
	return nil
}

// UseSmsCodeRPC 校验并消耗验证码。
func (s *Service) UseSmsCodeRPC(ctx context.Context, mobile, code string, scene int, ip string) error {
	if strings.TrimSpace(code) == "" {
		return badRequest("验证码")
	}
	if strings.TrimSpace(ip) == "" {
		return badRequest("使用 IP 不能为空")
	}
	return s.useSmsCode(ctx, mobile, code, scene, ip)
}

// ValidateSmsCodeRPC 只检查验证码仍可用，不标记已使用。
func (s *Service) ValidateSmsCodeRPC(ctx context.Context, mobile, code string, scene int) error {
	if strings.TrimSpace(code) == "" {
		return badRequest("验证码")
	}
	if s.Sms == nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	last, err := s.Sms.LastSmsCode(ctx, mobile, code, scene, true, true)
	if err != nil {
		return err
	}
	if last == nil {
		return &Error{Code: codeSmsNotFound, Msg: "验证码不存在"}
	}
	if s.now().Sub(last.CreateTime) >= s.smsExpire() {
		return &Error{Code: codeSmsExpired, Msg: "验证码已过期"}
	}
	if last.Used {
		return &Error{Code: codeSmsUsed, Msg: "验证码已使用"}
	}
	return nil
}

func smsSceneTemplate(scene int) (string, bool) {
	switch scene {
	case 1:
		return "user-sms-login", true
	case 2:
		return "user-update-mobile", true
	case 3:
		return "user-update-password", true
	case 4:
		return "user-reset-password", true
	case 21:
		return "admin-sms-login", true
	case 22:
		return "admin-sms-register", true
	case 23:
		return "admin-reset-password", true
	default:
		return "", false
	}
}

// Register 在注册开关打开且未超过租户配额时创建管理员并登录。
func (s *Service) Register(ctx context.Context, tenantID int64, username, nickname, password, captcha string, meta RequestMeta) (*LoginResult, error) {
	if s.CaptchaOn && (captcha == "" || (s.Captcha != nil && s.Captcha.Verify(ctx, captcha) != nil)) {
		return nil, &Error{Code: codeRegisterCaptcha, Msg: "验证码不正确，原因：验证码不能为空"}
	}
	if reason := registerFieldError(username, nickname, password); reason != "" {
		return nil, badRequest(reason)
	}
	enabled, err := s.Users.ConfigValue(ctx, "system.user.register-enabled")
	if err != nil {
		return nil, err
	}
	if enabled != "true" {
		return nil, &Error{Code: codeRegisterOff, Msg: "注册功能已关闭"}
	}
	count, err := s.Users.CountUsers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	limit, err := s.Users.AccountLimit(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if count >= limit {
		return nil, &Error{Code: codeUserCount, Msg: fmt.Sprintf("创建用户失败，原因：超过租户最大租户配额(%d)！", limit)}
	}
	existing, err := s.Users.FindByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, &Error{Code: codeUsernameTaken, Msg: "用户账号已经存在"}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user, err := s.Users.RegisterUser(ctx, tenantID, username, nickname, string(hash))
	if err != nil {
		return nil, err
	}
	if err := s.checkTenant(ctx, user.TenantID); err != nil {
		return nil, err
	}
	s.recordLogin(ctx, user.TenantID, user.ID, username, 100, 0, meta)
	return s.issue(ctx, user)
}

// ResetPassword 先消耗场景 23 的验证码，再更新密码。两者由存储层放在同一事务。
func (s *Service) ResetPassword(ctx context.Context, tenantID int64, mobile, code, password, ip string) error {
	if strings.TrimSpace(password) == "" {
		return badRequest("密码不能为空")
	}
	if n := len(password); n < 4 || n > 16 {
		return badRequest("密码长度为 4-16 位")
	}
	if strings.TrimSpace(mobile) == "" {
		return badRequest("手机号不能为空")
	}
	if !mobileOK(mobile) {
		return badRequest("手机号格式不正确")
	}
	if strings.TrimSpace(code) == "" {
		return badRequest("手机手机短信验证码不能为空")
	}
	user, err := s.Users.FindByMobile(ctx, tenantID, mobile)
	if err != nil {
		return err
	}
	if user == nil {
		return &Error{Code: codeMobileUnreg, Msg: "该手机号尚未注册"}
	}
	if err := s.useSmsCode(ctx, mobile, code, sceneReset, ip); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Users.UpdatePassword(ctx, user.TenantID, user.ID, string(hash))
}

func registerFieldError(username, nickname, password string) string {
	if strings.TrimSpace(username) == "" {
		return "用户账号不能为空"
	}
	if !usernameOK(username) {
		return "用户账号由 数字、字母 组成"
	}
	if utf8.RuneCountInString(nickname) == 0 {
		return "用户昵称不能为空"
	}
	if utf8.RuneCountInString(nickname) > 30 {
		return "用户昵称长度不能超过 30 个字符"
	}
	if password == "" {
		return "密码不能为空"
	}
	if n := len(password); n < 4 || n > 16 {
		return "密码长度为 4-16 位"
	}
	return ""
}

func usernameOK(username string) bool {
	if len(username) < 4 || len(username) > 30 {
		return false
	}
	for _, ch := range username {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func mobileOK(mobile string) bool {
	return mobilePattern.MatchString(mobile)
}

func sameShanghaiDay(a, b time.Time) bool {
	aa := a.In(shanghai)
	bb := b.In(shanghai)
	return aa.Year() == bb.Year() && aa.YearDay() == bb.YearDay()
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)
