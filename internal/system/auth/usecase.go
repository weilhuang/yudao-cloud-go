package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Service 是登录用例。它只依赖接口，MySQL 和 Redis 由 main 注入。
type Service struct {
	Users       UserStore
	Tokens      TokenStore
	Permissions PermissionStore
	Cache       TokenCache
	CaptchaOn   bool
	Now         func() time.Time
	Recorder    LoginRecorder
	Captcha     CaptchaChecker
	// ValidateTenant 对齐 Java 的 TenantSecurityWebFilter。生产组装必须注入；
	// 单元测试可按需要注入假实现，不由 auth 包依赖 directory 包。
	ValidateTenant func(ctx context.Context, tenantID int64, now time.Time) error
	// Sms 保存手机验证码。Codes 负责交给短信渠道；测试里必须是不访问外网的实现。
	Sms          SmsCodeStore
	Codes        CodeSender
	SmsExpire    time.Duration
	SmsFrequency time.Duration
	SmsDailyMax  int
	CodeBegin    int
	CodeEnd      int
}

// CaptchaChecker 校验滑块验证码的二次票据。登录开启验证码时才会调用。
type CaptchaChecker interface {
	Verify(ctx context.Context, verification string) error
}

// RequestMeta 是写登录日志需要的请求信息。
type RequestMeta struct {
	IP        string
	UserAgent string
	TraceID   string
}

// LoginRecord 对齐 system_login_log。Result 0 是成功，10 是账号密码错误，20 是禁用，31 是验证码错误。
type LoginRecord struct {
	TenantID  int64  `json:"tenantId"`
	LogType   int    `json:"logType"`
	TraceID   string `json:"traceId"`
	UserID    int64  `json:"userId"`
	UserType  int    `json:"userType"`
	Username  string `json:"username"`
	Result    int    `json:"result"`
	UserIP    string `json:"userIp"`
	UserAgent string `json:"userAgent"`
}

// LoginRecorder 由日志模块实现。登录失败也要记，所以用例在返回错误前调用它。
type LoginRecorder interface {
	RecordLogin(ctx context.Context, item LoginRecord) error
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// LoginResult 是登录和刷新令牌的返回。ExpiresTime 是毫秒时间戳。
type LoginResult struct {
	UserID       int64
	AccessToken  string
	RefreshToken string
	ExpiresTime  int64
}

// Login 校验账号密码并签发令牌。验证码关闭时不看 captchaVerification。
func (s *Service) Login(ctx context.Context, tenantID int64, username, password, captcha string, meta RequestMeta) (*LoginResult, error) {
	if s.CaptchaOn && (captcha == "" || (s.Captcha != nil && s.Captcha.Verify(ctx, captcha) != nil)) {
		s.recordLogin(ctx, tenantID, 0, username, 100, 31, meta)
		return nil, captchaMissing()
	}
	user, err := s.Users.FindByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		userID := int64(0)
		if user != nil {
			userID = user.ID
		}
		s.recordLogin(ctx, tenantID, userID, username, 100, 10, meta)
		return nil, badCredentials()
	}
	if user.Status != statusEnable {
		s.recordLogin(ctx, tenantID, user.ID, username, 100, 20, meta)
		return nil, userDisabled()
	}
	if err := s.checkTenant(ctx, user.TenantID); err != nil {
		return nil, err
	}
	if err := s.Users.TouchLogin(ctx, user.ID, meta.IP, s.now().UnixMilli()); err != nil {
		return nil, err
	}
	s.recordLogin(ctx, user.TenantID, user.ID, username, 100, 0, meta)
	return s.issue(ctx, user)
}

// Authenticate 只校验账号密码、状态和租户，不签发令牌。开放接口的密码模式使用它。
func (s *Service) Authenticate(ctx context.Context, tenantID int64, username, password string) (*User, error) {
	user, err := s.Users.FindByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return nil, badCredentials()
	}
	if user.Status != statusEnable {
		return nil, userDisabled()
	}
	if err := s.checkTenant(ctx, user.TenantID); err != nil {
		return nil, err
	}
	return user, nil
}

// Refresh 用刷新令牌换一张新的访问令牌。刷新令牌本身不变，直到过期。
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*LoginResult, error) {
	access, err := s.RefreshTokenRPC(ctx, refreshToken, clientIDDefault, -1)
	if err != nil {
		return nil, err
	}
	return &LoginResult{
		UserID:       access.UserID,
		AccessToken:  access.AccessToken,
		RefreshToken: access.RefreshToken,
		ExpiresTime:  access.ExpiresAt.UnixMilli(),
	}, nil
}

// refreshToken 在管理端和 Feign RPC 间共享令牌轮换逻辑；RPC 可指定客户端。
func (s *Service) refreshToken(ctx context.Context, refreshToken, clientID string, tenantID int64) (*Token, error) {
	current, err := s.Tokens.FindRefresh(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, badRequest("无效的刷新令牌")
	}
	client, err := s.clientByID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if current.ClientID != clientID {
		return nil, badRequest("刷新令牌的客户端编号不正确")
	}
	if tenantID >= 0 && current.TenantID != tenantID {
		// Java 的租户 SQL 拦截器会把其他租户的刷新令牌视为不存在。
		return nil, badRequest("无效的刷新令牌")
	}
	var user *User
	switch current.UserType {
	case userTypeAdmin:
		if current.UserID > 0 {
			user, err = s.Users.FindByID(ctx, current.UserID)
			if err != nil {
				return nil, err
			}
			if user == nil || user.TenantID != current.TenantID || user.Status != statusEnable {
				return nil, unauthorized("账号不存在或已被禁用")
			}
		}
	case userTypeMember:
		// mini 的会员令牌不读取管理用户表，也不授予管理端权限。
	default:
		return nil, badRequest("用户类型不正确")
	}
	if err := s.checkTenant(ctx, current.TenantID); err != nil {
		return nil, err
	}
	old, err := s.Tokens.DeleteAccessByRefresh(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if len(old) > 0 {
		if err := s.Cache.Delete(ctx, old...); err != nil {
			return nil, err
		}
	}
	if !current.ExpiresAt.After(s.now()) {
		if err := s.Tokens.DeleteRefresh(ctx, refreshToken); err != nil {
			return nil, err
		}
		return nil, unauthorized("刷新令牌已过期")
	}
	access, err := s.newAccess(ctx, current, user, client)
	if err != nil {
		return nil, err
	}
	return access, nil
}

// Logout 删除访问令牌和对应的刷新令牌。令牌不存在也算成功，和 Java 一致。
func (s *Service) Logout(ctx context.Context, accessToken string, meta RequestMeta) error {
	return s.removeToken(ctx, 0, accessToken, 200, meta)
}

// ForceLogout 是管理端强退。只删除当前租户的令牌，登录日志类型为 202。
func (s *Service) ForceLogout(ctx context.Context, tenantID int64, accessToken string, meta RequestMeta) error {
	return s.removeToken(ctx, tenantID, accessToken, 202, meta)
}

func (s *Service) removeToken(ctx context.Context, restrictTenant int64, accessToken string, logType int, meta RequestMeta) error {
	if accessToken == "" {
		return nil
	}
	token, err := s.Tokens.FindAccess(ctx, accessToken)
	if err != nil {
		return err
	}
	if token == nil {
		if restrictTenant > 0 {
			return nil
		}
		return s.Cache.Delete(ctx, accessToken)
	}
	if restrictTenant > 0 && token.TenantID != restrictTenant {
		return nil
	}
	if token.RefreshToken != "" {
		if err := s.Tokens.DeleteRefresh(ctx, token.RefreshToken); err != nil {
			return err
		}
	}
	if _, err := s.Tokens.DeleteAccess(ctx, accessToken); err != nil {
		return err
	}
	if err := s.Cache.Delete(ctx, accessToken, token.RefreshToken); err != nil {
		return err
	}
	username := ""
	if s.Recorder != nil && token.UserType == userTypeAdmin {
		if user, err := s.Users.FindByID(ctx, token.UserID); err == nil && user != nil {
			username = user.Username
		}
	}
	s.recordLoginAs(ctx, token.TenantID, token.UserID, token.UserType, username, logType, 0, meta)
	return nil
}

func (s *Service) recordLogin(ctx context.Context, tenantID, userID int64, username string, logType, result int, meta RequestMeta) {
	s.recordLoginAs(ctx, tenantID, userID, userTypeAdmin, username, logType, result, meta)
}

func (s *Service) recordLoginAs(ctx context.Context, tenantID, userID int64, userType int, username string, logType, result int, meta RequestMeta) {
	if s.Recorder == nil {
		return
	}
	_ = s.Recorder.RecordLogin(ctx, LoginRecord{
		TenantID: tenantID, LogType: logType, TraceID: meta.TraceID, UserID: userID,
		UserType: userType, Username: username, Result: result, UserIP: meta.IP, UserAgent: meta.UserAgent,
	})
}

// Check 给网关和其他服务校验令牌。数据库是有效性来源：缓存删除失败时，
// 已注销或已轮换的令牌也不能继续通过鉴权。Java 还允许报表和 WebSocket
// 使用未过期的刷新令牌作为访问凭据，所以访问表未命中时再查刷新表。
func (s *Service) Check(ctx context.Context, accessToken string) (*Token, error) {
	token, err := s.Tokens.FindAccess(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	fromRefresh := token == nil
	if token == nil {
		token, err = s.Tokens.FindRefresh(ctx, accessToken)
		if err != nil {
			return nil, err
		}
		if token == nil {
			return nil, unauthorized("访问令牌不存在")
		}
		token.AccessToken = accessToken
		token.UserInfo = map[string]string{}
	}
	if !token.ExpiresAt.After(s.now()) {
		return nil, unauthorized("访问令牌已过期")
	}
	switch token.UserType {
	case userTypeAdmin:
		if token.UserID > 0 {
			user, err := s.Users.FindByID(ctx, token.UserID)
			if err != nil {
				return nil, err
			}
			if user == nil || user.Status != statusEnable || user.TenantID != token.TenantID {
				return nil, unauthorized("账号不存在或已被禁用")
			}
			if fromRefresh {
				token.UserInfo = userInfo(user)
			}
		}
	case userTypeMember:
		// 会员数据归会员模块管理，不能用管理员表校验同号用户。
	default:
		return nil, unauthorized("用户类型不正确")
	}
	if err := s.checkTenant(ctx, token.TenantID); err != nil {
		return nil, err
	}
	return token, nil
}

func (s *Service) checkTenant(ctx context.Context, tenantID int64) error {
	if s.ValidateTenant == nil {
		return nil
	}
	return s.ValidateTenant(ctx, tenantID, s.now())
}

// Session 给其他管理接口复用登录态和权限集合。
func (s *Service) Session(ctx context.Context, accessToken string) (userID, tenantID int64, perms map[string]bool, err error) {
	if accessToken == "" {
		return 0, 0, nil, unauthorized("账号未登录")
	}
	token, err := s.Check(ctx, accessToken)
	if err != nil {
		return 0, 0, nil, err
	}
	if token.UserType != userTypeAdmin || token.UserID <= 0 {
		return 0, 0, nil, forbidden("您无权访问管理后台")
	}
	info, err := s.Permission(ctx, token.UserID)
	if err != nil {
		return 0, 0, nil, err
	}
	perms = map[string]bool{}
	if info != nil {
		for _, perm := range info.Permissions {
			perms[perm] = true
		}
	}
	return token.UserID, token.TenantID, perms, nil
}

// Permission 组装前端首页需要的用户、角色、权限和菜单树。
func (s *Service) Permission(ctx context.Context, userID int64) (*PermissionInfo, error) {
	user, err := s.Users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	roles, err := s.Permissions.RolesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	enabled := make([]Role, 0, len(roles))
	codes := make([]string, 0, len(roles))
	all := false
	var roleIDs []int64
	for _, role := range roles {
		if role.Status != statusEnable {
			continue
		}
		enabled = append(enabled, role)
		codes = append(codes, role.Code)
		roleIDs = append(roleIDs, role.ID)
		if role.Code == roleSuperAdmin {
			all = true
		}
	}
	menus, err := s.Permissions.MenusByRole(ctx, roleIDs, all)
	if err != nil {
		return nil, err
	}
	return &PermissionInfo{
		User:        *user,
		Roles:       codes,
		Permissions: permissionsOf(menus),
		Menus:       menuTree(menus),
	}, nil
}

// PermissionInfo 是 get-permission-info 的数据。
type PermissionInfo struct {
	User        User
	Roles       []string
	Permissions []string
	Menus       []MenuNode
}

// MenuNode 是给前端的菜单树。按钮不出现在这里。
type MenuNode struct {
	Menu
	Children []MenuNode
}

// IssueFor 给已经绑定的社交用户签发令牌，不再校验密码。
func (s *Service) IssueFor(ctx context.Context, userID int64) (*LoginResult, error) {
	user, err := s.Users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, unauthorized("账号未登录")
	}
	if user.Status != statusEnable {
		return nil, userDisabled()
	}
	if err := s.checkTenant(ctx, user.TenantID); err != nil {
		return nil, err
	}
	return s.issue(ctx, user)
}

func (s *Service) issue(ctx context.Context, user *User) (*LoginResult, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	refresh := Token{
		RefreshToken: newToken(),
		UserID:       user.ID,
		UserType:     userTypeAdmin,
		TenantID:     user.TenantID,
		ClientID:     client.ClientID,
		ExpiresAt:    s.now().Add(client.RefreshTTL),
	}
	access, err := s.issuePair(ctx, refresh, user, client)
	if err != nil {
		return nil, err
	}
	return &LoginResult{
		UserID:       user.ID,
		AccessToken:  access.AccessToken,
		RefreshToken: access.RefreshToken,
		ExpiresTime:  access.ExpiresAt.UnixMilli(),
	}, nil
}

func (s *Service) client(ctx context.Context) (*Client, error) {
	return s.clientByID(ctx, clientIDDefault)
}

func (s *Service) clientByID(ctx context.Context, clientID string) (*Client, error) {
	client, err := s.Tokens.Client(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, &Error{Code: codeClientNotFound, Msg: "OAuth2 客户端不存在"}
	}
	if client.Status != statusEnable {
		return nil, &Error{Code: codeClientDisabled, Msg: "OAuth2 客户端已禁用"}
	}
	return client, nil
}

func (s *Service) newAccess(ctx context.Context, refresh *Token, user *User, client *Client) (*Token, error) {
	access := s.buildAccess(refresh, user, client)
	if err := s.Tokens.InsertAccess(ctx, access); err != nil {
		return nil, err
	}
	// Java 会优先读共享 Redis。只在 MySQL 写入成功后发布缓存，避免 Java
	// 接受尚未提交或已回滚的令牌。缓存失败时补偿删除刚插入的令牌。
	if err := s.Cache.Put(ctx, access.AccessToken, access); err != nil {
		return nil, errors.Join(err, s.compensateIssued(ctx, access.AccessToken, ""))
	}
	return &access, nil
}

// issuePair 将两种令牌一次写入 MySQL，提交后再发布给 Java 共用的 Redis。
func (s *Service) issuePair(ctx context.Context, refresh Token, user *User, client *Client) (*Token, error) {
	access := s.buildAccess(&refresh, user, client)
	if err := s.Tokens.InsertPair(ctx, refresh, access); err != nil {
		return nil, err
	}
	if err := s.Cache.Put(ctx, access.AccessToken, access); err != nil {
		return nil, errors.Join(err, s.compensateIssued(ctx, access.AccessToken, refresh.RefreshToken))
	}
	return &access, nil
}

// compensateIssued 不沿用已取消的请求 Context；客户端断开时也尽量撤销
// 已提交但未成功发布缓存的令牌，并清理可能写成功却超时的 Redis key。
func (s *Service) compensateIssued(ctx context.Context, accessToken, refreshToken string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, accessErr := s.Tokens.DeleteAccess(cleanupCtx, accessToken)
	var refreshErr error
	if refreshToken != "" {
		refreshErr = s.Tokens.DeleteRefresh(cleanupCtx, refreshToken)
	}
	cacheErr := s.Cache.Delete(cleanupCtx, accessToken)
	return errors.Join(accessErr, refreshErr, cacheErr)
}

func (s *Service) buildAccess(refresh *Token, user *User, client *Client) Token {
	access := Token{
		AccessToken:  newToken(),
		RefreshToken: refresh.RefreshToken,
		UserID:       refresh.UserID,
		UserType:     refresh.UserType,
		TenantID:     refresh.TenantID,
		ClientID:     client.ClientID,
		Scopes:       refresh.Scopes,
		ExpiresAt:    s.now().Add(client.AccessTTL),
		UserInfo:     userInfo(user),
	}
	return access
}

// userInfo 与 Java buildUserInfo 的当前管理员字段对应；会员和服务令牌返回空对象。
func userInfo(user *User) map[string]string {
	info := map[string]string{}
	if user != nil {
		info["nickname"] = user.Nickname
		if user.DeptID != nil {
			info["deptId"] = itoa(*user.DeptID)
		}
	}
	return info
}

func permissionsOf(menus []Menu) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, menu := range menus {
		if menu.Status != statusEnable || menu.Permission == "" {
			continue
		}
		if _, ok := seen[menu.Permission]; ok {
			continue
		}
		seen[menu.Permission] = struct{}{}
		out = append(out, menu.Permission)
	}
	return out
}

func menuTree(menus []Menu) []MenuNode {
	nodes := map[int64]*MenuNode{}
	var order []int64
	for _, menu := range menus {
		if menu.Status != statusEnable || menu.Type == menuButton {
			continue
		}
		nodes[menu.ID] = &MenuNode{Menu: menu}
		order = append(order, menu.ID)
	}
	return collect(nodes, order)
}

func collect(nodes map[int64]*MenuNode, order []int64) []MenuNode {
	var roots []MenuNode
	for _, id := range order {
		node := nodes[id]
		if node.ParentID == 0 || nodes[node.ParentID] == nil {
			roots = append(roots, snapshot(*node, nodes))
		}
	}
	return roots
}

func snapshot(node MenuNode, nodes map[int64]*MenuNode) MenuNode {
	node.Children = nil
	for _, childID := range childOrder(node.ID, nodes) {
		node.Children = append(node.Children, snapshot(*nodes[childID], nodes))
	}
	return node
}

func childOrder(parent int64, nodes map[int64]*MenuNode) []int64 {
	var ids []int64
	for id, node := range nodes {
		if node.ParentID == parent {
			ids = append(ids, id)
		}
	}
	// map 迭代无序。按 Sort、ID 排，菜单顺序才稳定。
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, b := nodes[ids[i]], nodes[ids[j]]
			if a.Sort > b.Sort || (a.Sort == b.Sort && a.ID > b.ID) {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	return ids
}

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString(b[:])
	}
	return hex.EncodeToString(b[:])
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
