package auth

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestLoginRecordsBadCredentials(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	rec := &memRecorder{}
	svc := testService(string(hash), statusEnable)
	svc.Recorder = rec
	_, err := svc.Login(context.Background(), 1, "admin", "wrong-pass", "", RequestMeta{IP: "10.0.0.8", UserAgent: "ua"})
	if asError(err).Code != codeBadCredentials {
		t.Fatal(err)
	}
	if rec.last.Result != 10 || rec.last.LogType != 100 || rec.last.UserIP != "10.0.0.8" || rec.last.UserType != userTypeAdmin {
		t.Fatalf("%+v", rec.last)
	}
}

type memRecorder struct {
	last LoginRecord
}

func (m *memRecorder) RecordLogin(_ context.Context, item LoginRecord) error {
	m.last = item
	return nil
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	_, err := svc.Login(context.Background(), 1, "admin", "wrong-pass", "", RequestMeta{IP: "127.0.0.1"})
	biz := asError(err)
	if biz == nil || biz.Code != codeBadCredentials {
		t.Fatalf("err %#v", err)
	}
}

func TestLoginRejectsDisabledUser(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), 1)
	_, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	biz := asError(err)
	if biz == nil || biz.Code != codeUserDisabled {
		t.Fatalf("err %#v", err)
	}
}

func TestLoginRequiresCaptchaWhenEnabled(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	svc.CaptchaOn = true
	_, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	if asError(err).Code != codeCaptcha {
		t.Fatal(err)
	}
}

func TestLoginIssuesToken(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	got, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken == "" || got.RefreshToken == "" || got.UserID != 7 {
		t.Fatalf("%+v", got)
	}
	checked, err := svc.Check(context.Background(), got.AccessToken)
	if err != nil || checked.TenantID != 1 {
		t.Fatalf("check %+v %v", checked, err)
	}
}

// Java 允许报表和 WebSocket 直接携带刷新令牌；吊销后两种用途都必须失效。
func TestCheckRefreshTokenFallbackAndLogout(t *testing.T) {
	svc := testService("unused", statusEnable)
	issued, err := svc.IssueFor(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := svc.Check(context.Background(), issued.RefreshToken)
	if err != nil || checked.AccessToken != issued.RefreshToken || checked.UserID != 7 ||
		checked.UserInfo["nickname"] != "管理员" || checked.UserInfo["deptId"] != "10" {
		t.Fatalf("刷新令牌回退失败: %+v, %v", checked, err)
	}
	store := svc.Tokens.(*memTokens)
	refresh := store.refresh[issued.RefreshToken]
	refresh.ExpiresAt = time.Now().Add(-time.Minute)
	store.refresh[issued.RefreshToken] = refresh
	if _, err := svc.Check(context.Background(), issued.RefreshToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("过期刷新令牌仍可使用: %v", err)
	}
	refresh.ExpiresAt = time.Now().Add(time.Hour)
	store.refresh[issued.RefreshToken] = refresh
	// 模拟 Java 将刷新令牌回退结果写入同名 Redis key。
	svc.Cache.(*memCache).items[issued.RefreshToken] = refresh
	if err := svc.Logout(context.Background(), issued.AccessToken, RequestMeta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Check(context.Background(), issued.RefreshToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("注销后刷新令牌仍可使用: %v", err)
	}
	if _, ok := svc.Cache.(*memCache).items[issued.RefreshToken]; ok {
		t.Fatal("注销后残留 Java 刷新令牌回退缓存")
	}
}

func TestRefreshExpired(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.Local)
	svc.Now = func() time.Time { return now }
	got, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return now.Add(40 * 24 * time.Hour) }
	_, err = svc.Refresh(context.Background(), got.RefreshToken)
	if asError(err).Code != codeUnauthorized {
		t.Fatal(err)
	}
}

func TestExpiredRefreshRevokesStillLiveAccess(t *testing.T) {
	svc := testService("unused", statusEnable)
	svc.Tokens.(*memTokens).client.AccessTTL = 48 * time.Hour
	svc.Tokens.(*memTokens).client.RefreshTTL = time.Hour
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.Local)
	svc.Now = func() time.Time { return now }
	first, err := svc.IssueFor(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := svc.Refresh(context.Background(), first.RefreshToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("过期刷新令牌未拒绝: %v", err)
	}
	if _, err := svc.Check(context.Background(), first.AccessToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("刷新令牌过期后旧访问令牌仍有效: %v", err)
	}
	if token, err := svc.Tokens.FindRefresh(context.Background(), first.RefreshToken); err != nil || token != nil {
		t.Fatalf("过期刷新令牌仍存在: %+v, %v", token, err)
	}
}

func TestLogoutThenCheckIsUnauthorized(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	got, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(context.Background(), got.AccessToken, RequestMeta{IP: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Check(context.Background(), got.AccessToken)
	if asError(err).Code != codeUnauthorized {
		t.Fatal(err)
	}
}

// Redis 删除失败后旧 key 可能还在，鉴权仍必须以数据库的撤销结果为准。
func TestLogoutCacheFailureCannotKeepAccessValid(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	got, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	svc.Cache = &failingDeleteCache{memCache: svc.Cache.(*memCache)}
	if err := svc.Logout(context.Background(), got.AccessToken, RequestMeta{}); err == nil {
		t.Fatal("缓存删除失败应报告给调用方")
	}
	if _, err := svc.Check(context.Background(), got.AccessToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("已注销令牌不得因缓存残留而继续有效: %v", err)
	}
	if refresh, err := svc.Tokens.FindRefresh(context.Background(), got.RefreshToken); err != nil || refresh != nil {
		t.Fatalf("登出后刷新令牌仍有效: %+v, %v", refresh, err)
	}
}

func TestRefreshRejectsDisabledUserWithoutDeletingOldAccess(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	got, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	svc.Users.(*memUsers).user.Status = 1
	if _, err := svc.Refresh(context.Background(), got.RefreshToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("被禁用账号不应刷新令牌: %v", err)
	}
	if _, err := svc.Check(context.Background(), got.AccessToken); asError(err).Code != codeUnauthorized {
		t.Fatalf("被禁用账号的现有令牌也应失效: %v", err)
	}
}

func TestDisabledTenantRejectsLoginAndExistingToken(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	disabled := false
	svc.ValidateTenant = func(_ context.Context, id int64, _ time.Time) error {
		if id != 1 {
			t.Fatalf("错误的租户编号: %d", id)
		}
		if disabled {
			return &Error{Code: 1_002_015_001, Msg: "租户已禁用"}
		}
		return nil
	}
	got, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	disabled = true
	if _, err := svc.Login(context.Background(), 1, "admin", "right-pass", "", RequestMeta{}); asError(err).Code != 1_002_015_001 {
		t.Fatalf("禁用租户仍可登录: %v", err)
	}
	if _, err := svc.Check(context.Background(), got.AccessToken); asError(err).Code != 1_002_015_001 {
		t.Fatalf("禁用租户的旧令牌仍可访问: %v", err)
	}
	if _, err := svc.Refresh(context.Background(), got.RefreshToken); asError(err).Code != 1_002_015_001 {
		t.Fatalf("禁用租户仍可刷新令牌: %v", err)
	}
}

type failingDeleteCache struct{ *memCache }

func (f *failingDeleteCache) Delete(context.Context, ...string) error {
	return errors.New("Redis 不可用")
}

type failingPutCache struct{ *memCache }

func (f *failingPutCache) Put(context.Context, string, Token) error {
	return errors.New("Redis 不可用")
}

// 缓存不可用时，先提交的 MySQL 令牌必须经补偿删除，避免孤儿令牌。
func TestIssueCacheFailureLeavesNoUsableToken(t *testing.T) {
	svc := testService("unused", statusEnable)
	svc.Cache = &failingPutCache{memCache: svc.Cache.(*memCache)}
	if _, err := svc.IssueFor(context.Background(), 7); err == nil {
		t.Fatal("缓存失败应中止签发")
	}
	store := svc.Tokens.(*memTokens)
	if len(store.refresh) != 0 || len(store.access) != 0 {
		t.Fatalf("签发失败后留下令牌: refresh=%d access=%d", len(store.refresh), len(store.access))
	}
	if _, err := svc.CreateAccessToken(context.Background(), 1, 7, userTypeAdmin, "default", nil); err == nil {
		t.Fatal("RPC 签发也应中止")
	}
	if len(store.refresh) != 0 || len(store.access) != 0 {
		t.Fatalf("RPC 签发失败后留下令牌: refresh=%d access=%d", len(store.refresh), len(store.access))
	}
}

type contextCheckingTokens struct{ *memTokens }

func (m *contextCheckingTokens) DeleteAccess(ctx context.Context, access string) (*Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.memTokens.DeleteAccess(ctx, access)
}

func (m *contextCheckingTokens) DeleteRefresh(ctx context.Context, refresh string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.memTokens.DeleteRefresh(ctx, refresh)
}

func TestIssueCompensatesAfterRequestCancellation(t *testing.T) {
	svc := testService("unused", statusEnable)
	store := svc.Tokens.(*memTokens)
	svc.Tokens = &contextCheckingTokens{memTokens: store}
	svc.Cache = &failingPutCache{memCache: svc.Cache.(*memCache)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.IssueFor(ctx, 7); err == nil {
		t.Fatal("缓存失败应中止签发")
	}
	if len(store.refresh) != 0 || len(store.access) != 0 {
		t.Fatalf("请求取消后补偿失败: refresh=%d access=%d", len(store.refresh), len(store.access))
	}
}

type failingPairTokens struct{ *memTokens }

func (*failingPairTokens) InsertPair(context.Context, Token, Token) error {
	return errors.New("MySQL 事务回滚")
}

func TestIssueTransactionFailureDoesNotPublishCache(t *testing.T) {
	svc := testService("unused", statusEnable)
	store := svc.Tokens.(*memTokens)
	svc.Tokens = &failingPairTokens{memTokens: store}
	if _, err := svc.CreateAccessToken(context.Background(), 1, 7, userTypeAdmin, "default", nil); err == nil {
		t.Fatal("事务失败应中止签发")
	}
	if len(store.refresh) != 0 || len(store.access) != 0 || len(svc.Cache.(*memCache).items) != 0 {
		t.Fatalf("事务失败后仍有令牌或缓存: refresh=%d access=%d cache=%d",
			len(store.refresh), len(store.access), len(svc.Cache.(*memCache).items))
	}
}

type failingAccessTokens struct{ *memTokens }

func (*failingAccessTokens) InsertAccess(context.Context, Token) error {
	return errors.New("MySQL 插入失败")
}

func TestRefreshInsertFailureDoesNotPublishCache(t *testing.T) {
	svc := testService("unused", statusEnable)
	first, err := svc.IssueFor(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	store := svc.Tokens.(*memTokens)
	svc.Tokens = &failingAccessTokens{memTokens: store}
	if _, err := svc.Refresh(context.Background(), first.RefreshToken); err == nil {
		t.Fatal("新访问令牌插入失败应返回错误")
	}
	if len(store.access) != 0 || len(svc.Cache.(*memCache).items) != 0 || len(store.refresh) != 1 {
		t.Fatalf("刷新失败后状态错误: access=%d cache=%d refresh=%d",
			len(store.access), len(svc.Cache.(*memCache).items), len(store.refresh))
	}
}

func TestPermissionTreeSkipsButtons(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("right-pass"), 4)
	svc := testService(string(hash), statusEnable)
	info, err := svc.Permission(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Roles) != 1 || info.Roles[0] != "common" {
		t.Fatalf("roles %+v", info.Roles)
	}
	if len(info.Permissions) != 1 || info.Permissions[0] != "system:user:query" {
		t.Fatalf("permissions %+v", info.Permissions)
	}
	if len(info.Menus) != 1 || info.Menus[0].Name != "系统" || len(info.Menus[0].Children) != 1 {
		t.Fatalf("menus %+v", info.Menus)
	}
	if info.Menus[0].Children[0].Name != "用户" {
		t.Fatalf("child %+v", info.Menus[0].Children[0])
	}
}

func testService(hash string, status int) *Service {
	dept := int64(10)
	users := &memUsers{user: &User{
		ID: 7, TenantID: 1, Username: "admin", Password: hash, Nickname: "管理员",
		DeptID: &dept, Status: status,
	}}
	return &Service{
		Users:       users,
		Tokens:      &memTokens{client: &Client{ClientID: clientIDDefault, AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour}},
		Permissions: &memPerms{},
		Cache:       &memCache{items: map[string]Token{}},
	}
}

type memUsers struct{ user *User }

func (m *memUsers) FindByUsername(context.Context, int64, string) (*User, error) {
	return m.user, nil
}
func (m *memUsers) FindByMobile(context.Context, int64, string) (*User, error) { return nil, nil }
func (m *memUsers) FindByID(context.Context, int64) (*User, error)             { return m.user, nil }
func (m *memUsers) TouchLogin(context.Context, int64, string, int64) error     { return nil }
func (m *memUsers) CountUsers(context.Context, int64) (int64, error)           { return 0, nil }
func (m *memUsers) AccountLimit(context.Context, int64) (int64, error)         { return 0, nil }
func (m *memUsers) ConfigValue(context.Context, string) (string, error)        { return "", nil }
func (m *memUsers) RegisterUser(context.Context, int64, string, string, string) (*User, error) {
	return nil, nil
}
func (m *memUsers) UpdatePassword(context.Context, int64, int64, string) error { return nil }

type memTokens struct {
	client  *Client
	clients map[string]*Client
	access  map[string]Token
	refresh map[string]Token
}

func (m *memTokens) Client(_ context.Context, clientID string) (*Client, error) {
	if m.clients != nil {
		return m.clients[clientID], nil
	}
	if m.client != nil && m.client.ClientID == clientID {
		return m.client, nil
	}
	return nil, nil
}
func (m *memTokens) InsertAccess(_ context.Context, token Token) error {
	if m.access == nil {
		m.access = map[string]Token{}
	}
	m.access[token.AccessToken] = token
	return nil
}
func (m *memTokens) InsertPair(_ context.Context, refresh, access Token) error {
	// 内存替身一次更新两个 map，模拟 MySQL 事务提交。
	if m.refresh == nil {
		m.refresh = map[string]Token{}
	}
	if m.access == nil {
		m.access = map[string]Token{}
	}
	m.refresh[refresh.RefreshToken] = refresh
	m.access[access.AccessToken] = access
	return nil
}
func (m *memTokens) FindAccess(_ context.Context, accessToken string) (*Token, error) {
	token, ok := m.access[accessToken]
	if !ok {
		return nil, nil
	}
	return &token, nil
}
func (m *memTokens) FindRefresh(_ context.Context, refreshToken string) (*Token, error) {
	token, ok := m.refresh[refreshToken]
	if !ok {
		return nil, nil
	}
	return &token, nil
}
func (m *memTokens) DeleteAccessByRefresh(_ context.Context, refresh string) ([]string, error) {
	var old []string
	for key, token := range m.access {
		if token.RefreshToken == refresh {
			old = append(old, key)
			delete(m.access, key)
		}
	}
	return old, nil
}
func (m *memTokens) DeleteRefresh(_ context.Context, refresh string) error {
	delete(m.refresh, refresh)
	return nil
}
func (m *memTokens) DeleteAccess(_ context.Context, access string) (*Token, error) {
	token, ok := m.access[access]
	if !ok {
		return nil, nil
	}
	delete(m.access, access)
	return &token, nil
}
func (m *memTokens) AccessesByUser(_ context.Context, tenantID, userID int64, userType int) ([]Token, error) {
	var out []Token
	for _, token := range m.access {
		if token.TenantID == tenantID && token.UserID == userID && token.UserType == userType {
			out = append(out, token)
		}
	}
	return out, nil
}

func (m *memTokens) AccessPage(_ context.Context, tenantID int64, query AccessTokenQuery, now time.Time) (AccessTokenPage, error) {
	matched := make([]Token, 0)
	for _, token := range m.access {
		if tokenVisible(token, tenantID, query, now) {
			matched = append(matched, token)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID > matched[j].ID })
	pageNo, pageSize := accessPageBounds(query)
	start := (pageNo - 1) * pageSize
	if start > len(matched) {
		start = len(matched)
	}
	end := start + pageSize
	if end > len(matched) {
		end = len(matched)
	}
	list := make([]AccessTokenItem, 0, end-start)
	for _, token := range matched[start:end] {
		list = append(list, AccessTokenItem{
			ID: token.ID, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
			UserID: token.UserID, UserType: token.UserType, ClientID: token.ClientID,
			CreateTime: token.CreatedAt.UnixMilli(), ExpiresTime: token.ExpiresAt.UnixMilli(),
		})
	}
	return AccessTokenPage{List: list, Total: int64(len(matched))}, nil
}

type memPerms struct{}

func (memPerms) RolesByUser(context.Context, int64) ([]Role, error) {
	return []Role{{ID: 1, Code: "common", Status: statusEnable}}, nil
}
func (memPerms) MenusByRole(context.Context, []int64, bool) ([]Menu, error) {
	return []Menu{
		{ID: 1, Name: "系统", Type: menuDir, Sort: 1, Status: statusEnable},
		{ID: 3, ParentID: 2, Name: "查询", Type: menuButton, Permission: "system:user:query", Status: statusEnable},
		{ID: 2, ParentID: 1, Name: "用户", Type: menuMenu, Sort: 1, Status: statusEnable},
	}, nil
}

type memCache struct{ items map[string]Token }

func (m *memCache) Put(_ context.Context, access string, token Token) error {
	m.items[access] = token
	return nil
}
func (m *memCache) Get(_ context.Context, access string) (*Token, error) {
	token, ok := m.items[access]
	if !ok {
		return nil, nil
	}
	return &token, nil
}
func (m *memCache) Delete(_ context.Context, tokens ...string) error {
	for _, token := range tokens {
		delete(m.items, token)
	}
	return nil
}
