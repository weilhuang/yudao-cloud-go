package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Store 读写 OAuth2 客户端和社交账号。
type Store interface {
	ClientByID(ctx context.Context, id int64) (*OAuthClient, error)
	ClientByClientID(ctx context.Context, clientID string) (*OAuthClient, error)
	ClientIDTaken(ctx context.Context, clientID string, exceptID int64) (bool, error)
	CreateClient(ctx context.Context, item OAuthClient) (int64, error)
	UpdateClient(ctx context.Context, item OAuthClient) error
	DeleteClient(ctx context.Context, id int64) error

	SocialByID(ctx context.Context, tenantID, id int64) (*SocialClient, error)
	SocialByType(ctx context.Context, tenantID int64, socialType, userType int) (*SocialClient, error)
	SocialTaken(ctx context.Context, tenantID int64, socialType, userType int, exceptID int64) (bool, error)
	CreateSocial(ctx context.Context, tenantID int64, item SocialClient) (int64, error)
	UpdateSocial(ctx context.Context, tenantID int64, item SocialClient) error
	DeleteSocial(ctx context.Context, tenantID, id int64) error

	SocialUserByOpenID(ctx context.Context, tenantID int64, socialType int, openID string) (*SocialUser, error)
	SocialUserByCodeState(ctx context.Context, tenantID int64, socialType int, code, state string) (*SocialUser, error)
	SocialUserByID(ctx context.Context, tenantID, id int64) (*SocialUserDetail, error)
	BindUserID(ctx context.Context, tenantID int64, socialType int, openID string) (int64, bool, error)
	SaveSocialUser(ctx context.Context, tenantID int64, item SocialUser) (int64, error)
	Bind(ctx context.Context, tenantID, userID int64, userType, socialType int, socialUserID int64) error
	// Rebind 先解开这个社交账号和该用户在此平台上的旧绑定，再写入新关系。
	Rebind(ctx context.Context, tenantID, userID int64, userType, socialType int, socialUserID int64) error
	Unbind(ctx context.Context, tenantID, userID int64, userType, socialType int) error
	SocialByBind(ctx context.Context, tenantID, userID int64, userType, socialType int) (*SocialUser, error)
	BoundUserID(ctx context.Context, tenantID int64, userType int, socialUserID int64) (*int64, error)

	Approves(ctx context.Context, tenantID, userID int64, userType int, clientID string, now time.Time) ([]Approve, error)
	SaveApprove(ctx context.Context, tenantID, userID int64, userType int, clientID, scope string, approved bool, expires time.Time) error
	InsertCode(ctx context.Context, tenantID, userID int64, userType int, clientID string, scopes []string, redirectURI, state string, expires time.Time) (string, error)
	ConsumeCode(ctx context.Context, code string, now time.Time) (*AuthCode, error)
}

// Profile 是授权码换回来的社交用户。
type Profile struct {
	OpenID   string
	Nickname string
	Avatar   string
}

// AuthCode 是尚未消费的授权码。
type AuthCode struct {
	UserID      int64
	UserType    int
	TenantID    int64
	ClientID    string
	Scopes      []string
	RedirectURI string
	State       string
}

// OpenToken 是开放接口返回的访问令牌。
type OpenToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Scopes       []string
	UserID       int64
	UserType     int
	TenantID     int64
	ClientID     string
}

// OpenGrants 把令牌签发委托给登录服务，避免身份包直接依赖令牌存储。
type OpenGrants struct {
	Issue    func(ctx context.Context, tenantID, userID int64, userType int, clientID string, scopes []string) (OpenToken, error)
	Refresh  func(ctx context.Context, refreshToken, clientID string) (OpenToken, error)
	Password func(ctx context.Context, tenantID int64, username, password, clientID string, scopes []string) (OpenToken, error)
	Check    func(ctx context.Context, accessToken string) (OpenToken, error)
	Revoke   func(ctx context.Context, clientID, accessToken string) (bool, error)
}
type Approve struct {
	Scope    string
	Approved bool
}
type Exchanger func(ctx context.Context, client SocialClient, code, redirectURI string) (Profile, error)

// Service 维护客户端，并完成社交登录前的绑定查找。
type Service struct {
	Store        Store
	StateStore   StateStore
	Exchange     Exchanger
	HTTP         *http.Client
	GiteeBase    string
	WechatBase   string
	DingTalkBase string
	WeComBase    string
	// AfterCommit 在 OAuth2 客户端批量删除已写入后清空 Java 的 oauth_client 缓存。
	AfterCommit func(ctx context.Context, name string) error
	// IssueAccess 给简化模式签发访问令牌。为空时不能完成 token 响应类型。
	IssueAccess IssueAccess
	// Grants 提供授权码、密码、客户端和刷新模式的令牌签发。
	Grants OpenGrants
	// WxEnvVersion 是小程序码打开的版本，空值按 release。
	WxEnvVersion string
	// WxMiniState 是订阅消息跳转的小程序版本，空值按 formal。
	WxMiniState string
	// WxNow、WxNonce、WxSleep 只给测试固定时间和跳过发货重试等待。
	WxNow    func() time.Time
	WxNonce  func() string
	WxSleep  func(time.Duration)
	wxTokens sync.Map
}

func (s *Service) evictCommitted(ctx context.Context, name string) error {
	if s == nil || s.AfterCommit == nil {
		return nil
	}
	if err := s.AfterCommit(ctx, name); err != nil {
		return &Error{Code: 500, Msg: "数据库已提交，但 Java 缓存失效失败；请检查 Redis 并补偿清理缓存"}
	}
	return nil
}

// SaveClient 创建或修改 OAuth2 客户端。clientId 不能重复。
func (s *Service) SaveClient(ctx context.Context, item OAuthClient) (int64, error) {
	if item.ClientID == "" || item.Name == "" {
		return 0, &Error{Code: 400, Msg: "客户端编号和名称不能为空"}
	}
	if item.ID == 0 && item.Secret == "" {
		return 0, &Error{Code: 400, Msg: "客户端密钥不能为空"}
	}
	taken, err := s.Store.ClientIDTaken(ctx, item.ClientID, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_020_001, Msg: "OAuth2 客户端编号已存在"}
	}
	if item.ID == 0 {
		return s.Store.CreateClient(ctx, item)
	}
	current, err := s.Store.ClientByID(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_002_020_000, Msg: "OAuth2 客户端不存在"}
	}
	if item.Secret == "" {
		item.Secret = current.Secret
	}
	return item.ID, s.Store.UpdateClient(ctx, item)
}

// DeleteClient 不存在时返回业务错误。
func (s *Service) DeleteClient(ctx context.Context, id int64) error {
	current, err := s.Store.ClientByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_020_000, Msg: "OAuth2 客户端不存在"}
	}
	return s.Store.DeleteClient(ctx, id)
}

// SaveSocial 创建或修改社交客户端。
func (s *Service) SaveSocial(ctx context.Context, tenantID int64, item SocialClient) (int64, error) {
	if item.Name == "" || item.ClientID == "" || item.ClientSecret == "" {
		return 0, &Error{Code: 400, Msg: "应用名、客户端编号和密钥不能为空"}
	}
	taken, err := s.Store.SocialTaken(ctx, tenantID, item.SocialType, item.UserType, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_018_211, Msg: "社交客户端已存在配置"}
	}
	if item.ID == 0 {
		return s.Store.CreateSocial(ctx, tenantID, item)
	}
	current, err := s.Store.SocialByID(ctx, tenantID, item.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_002_018_210, Msg: "社交客户端不存在"}
	}
	return item.ID, s.Store.UpdateSocial(ctx, tenantID, item)
}

// Redirect 生成授权地址，并把随机 state 与本次授权的参数一起存入 Redis。
func (s *Service) Redirect(ctx context.Context, tenantID int64, socialType, userType int, redirectURI string) (string, error) {
	client, err := s.enabledClient(ctx, tenantID, socialType, userType)
	if err != nil {
		return "", err
	}
	if s.StateStore == nil {
		return "", fmt.Errorf("社交授权 state 存储未配置")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成社交授权 state: %w", err)
	}
	state := hex.EncodeToString(buf)
	link, err := AuthorizeURL(client.SocialType, client.ClientID, redirectURI, state)
	if err != nil {
		return "", err
	}
	value, err := json.Marshal(socialState{TenantID: tenantID, SocialType: socialType, UserType: userType, RedirectURI: redirectURI})
	if err != nil {
		return "", err
	}
	if err := s.StateStore.Set(ctx, stateKey(state), string(value), socialStateTTL); err != nil {
		return "", fmt.Errorf("保存社交授权 state: %w", err)
	}
	return link, nil
}

// LoginUser 用授权码找到已绑定的本地用户。没绑定返回 1002018001。
func (s *Service) LoginUser(ctx context.Context, tenantID int64, socialType, userType int, code, state string) (int64, error) {
	if code == "" || state == "" {
		return 0, &Error{Code: 400, Msg: "授权码和 state 不能为空"}
	}
	redirectURI, err := s.consumeState(ctx, tenantID, socialType, userType, state)
	if err != nil {
		return 0, err
	}
	client, err := s.enabledClient(ctx, tenantID, socialType, userType)
	if err != nil {
		return 0, err
	}
	if s.Exchange == nil {
		return 0, &Error{Code: 1_002_018_000, Msg: "社交授权失败，原因是：该平台的授权码兑换尚未接入"}
	}
	profile, err := s.Exchange(ctx, *client, code, redirectURI)
	if err != nil || profile.OpenID == "" {
		reason := "授权码无效"
		if err != nil {
			reason = err.Error()
		}
		return 0, &Error{Code: 1_002_018_000, Msg: fmt.Sprintf("社交授权失败，原因是：%s", reason)}
	}
	userID, ok, err := s.Store.BindUserID(ctx, tenantID, socialType, profile.OpenID)
	if err != nil {
		return 0, err
	}
	if !ok {
		if _, err := s.Store.SaveSocialUser(ctx, tenantID, SocialUser{Type: socialType, OpenID: profile.OpenID, Nickname: profile.Nickname, Avatar: profile.Avatar}); err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_018_001, Msg: "社交授权失败，找不到对应的用户"}
	}
	return userID, nil
}

// BindCurrentUser 把授权码对应的社交账号绑到当前管理员。
// 同一个 code 和 state 已落库时不再向社交平台兑换，方便登录未绑定后再点绑定。
func (s *Service) BindCurrentUser(ctx context.Context, tenantID, userID int64, socialType int, code, state string) error {
	_, err := s.BindUser(ctx, tenantID, userID, 2, socialType, code, state)
	return err
}

// UnbindCurrentUser 按 openid 确认社交用户存在后，解除当前管理员在该平台的绑定。
func (s *Service) UnbindCurrentUser(ctx context.Context, tenantID, userID int64, socialType int, openID string) error {
	return s.UnbindUser(ctx, tenantID, userID, 2, socialType, openID)
}

// UnbindUser 先确认 openid 对应的社交用户存在，再按用户类型解开这一平台的绑定。
func (s *Service) UnbindUser(ctx context.Context, tenantID, userID int64, userType, socialType int, openID string) error {
	user, err := s.Store.SocialUserByOpenID(ctx, tenantID, socialType, openID)
	if err != nil {
		return err
	}
	if user == nil {
		return &Error{Code: 1_002_018_001, Msg: "社交授权失败，找不到对应的用户"}
	}
	return s.Store.Unbind(ctx, tenantID, userID, userType, socialType)
}

// BindUser 把授权码对应的社交账号绑到指定用户，并返回 openid。用户类型由调用方传入。
func (s *Service) BindUser(ctx context.Context, tenantID, userID int64, userType, socialType int, code, state string) (string, error) {
	if code == "" || state == "" {
		return "", &Error{Code: 400, Msg: "授权码和 state 不能为空"}
	}
	existing, err := s.Store.SocialUserByCodeState(ctx, tenantID, socialType, code, state)
	if err != nil {
		return "", err
	}
	socialUserID := int64(0)
	openID := ""
	if existing != nil {
		socialUserID = existing.ID
		openID = existing.OpenID
	} else {
		saved, err := s.exchangeSocialUser(ctx, tenantID, userType, socialType, code, state)
		if err != nil {
			return "", err
		}
		socialUserID = saved.ID
		openID = saved.OpenID
	}
	if err := s.Store.Rebind(ctx, tenantID, userID, userType, socialType, socialUserID); err != nil {
		return "", err
	}
	return openID, nil
}

// SocialView 是 Feign 返回的社交用户。未绑定时 UserID 为空。
type SocialView struct {
	OpenID   string
	Nickname string
	Avatar   string
	UserID   *int64
}

// SocialByUser 按用户、用户类型和社交平台查已绑定的社交账号。没有绑定时返回 nil。
func (s *Service) SocialByUser(ctx context.Context, tenantID, userID int64, userType, socialType int) (*SocialView, error) {
	item, err := s.Store.SocialByBind(ctx, tenantID, userID, userType, socialType)
	if err != nil || item == nil {
		return nil, err
	}
	bound := item.UserID
	return &SocialView{OpenID: item.OpenID, Nickname: item.Nickname, Avatar: item.Avatar, UserID: &bound}, nil
}

// SocialByCode 兑换或复用授权码对应的社交用户。未绑定时 UserID 为空。
func (s *Service) SocialByCode(ctx context.Context, tenantID int64, userType, socialType int, code, state string) (*SocialView, error) {
	existing, err := s.Store.SocialUserByCodeState(ctx, tenantID, socialType, code, state)
	if err != nil {
		return nil, err
	}
	item := existing
	if item == nil {
		item, err = s.exchangeSocialUser(ctx, tenantID, userType, socialType, code, state)
		if err != nil {
			return nil, err
		}
	}
	userID, err := s.Store.BoundUserID(ctx, tenantID, userType, item.ID)
	if err != nil {
		return nil, err
	}
	return &SocialView{OpenID: item.OpenID, Nickname: item.Nickname, Avatar: item.Avatar, UserID: userID}, nil
}

func (s *Service) exchangeSocialUser(ctx context.Context, tenantID int64, userType, socialType int, code, state string) (*SocialUser, error) {
	redirectURI, err := s.consumeState(ctx, tenantID, socialType, userType, state)
	if err != nil {
		return nil, err
	}
	client, err := s.enabledClient(ctx, tenantID, socialType, userType)
	if err != nil {
		return nil, err
	}
	if s.Exchange == nil {
		return nil, &Error{Code: 1_002_018_000, Msg: "社交授权失败，原因是：该平台的授权码兑换尚未接入"}
	}
	profile, err := s.Exchange(ctx, *client, code, redirectURI)
	if err != nil || profile.OpenID == "" {
		reason := "授权码无效"
		if err != nil {
			reason = err.Error()
		}
		return nil, &Error{Code: 1_002_018_000, Msg: "社交授权失败，原因是：" + reason}
	}
	id, err := s.Store.SaveSocialUser(ctx, tenantID, SocialUser{
		Type: socialType, OpenID: profile.OpenID, Nickname: profile.Nickname, Avatar: profile.Avatar,
		Code: code, State: state,
	})
	if err != nil {
		return nil, err
	}
	return &SocialUser{ID: id, Type: socialType, OpenID: profile.OpenID, Nickname: profile.Nickname, Avatar: profile.Avatar}, nil
}

func (s *Service) enabledClient(ctx context.Context, tenantID int64, socialType, userType int) (*SocialClient, error) {
	client, err := s.Store.SocialByType(ctx, tenantID, socialType, userType)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, &Error{Code: 1_002_018_210, Msg: "社交客户端不存在"}
	}
	if client.Status != 0 {
		return nil, &Error{Code: 1_002_018_000, Msg: "社交授权失败，原因是：社交客户端已关闭"}
	}
	return client, nil
}
