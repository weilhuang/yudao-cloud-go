package identity

import (
	"context"
	"testing"
	"time"
)

func TestSaveClientRejectsDuplicate(t *testing.T) {
	svc := &Service{Store: &memStore{clientTaken: true}}
	_, err := svc.SaveClient(context.Background(), OAuthClient{ClientID: "default", Secret: "s", Name: "默认"})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_020_001 {
		t.Fatal(err)
	}
}

func TestSocialLoginRequiresBind(t *testing.T) {
	svc := &Service{
		Store:      &memStore{social: &SocialClient{ID: 1, SocialType: 32, UserType: 2, ClientID: "wx", ClientSecret: "sec", Status: 0}},
		StateStore: newMemStateStore(),
		Exchange: func(context.Context, SocialClient, string, string) (Profile, error) {
			return Profile{OpenID: "openid-1", Nickname: "张三"}, nil
		},
	}
	state := mustRedirectState(t, svc, 1, 32, "https://app.example.com/callback")
	_, err := svc.LoginUser(context.Background(), 1, 32, 2, "code", state)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_018_001 || !svc.Store.(*memStore).saved {
		t.Fatal(err)
	}
}

func TestSocialLoginReturnsBoundUser(t *testing.T) {
	svc := &Service{
		Store:      &memStore{social: &SocialClient{SocialType: 10, UserType: 2, Status: 0, ClientID: "gitee"}, boundUser: 8},
		StateStore: newMemStateStore(),
		Exchange: func(context.Context, SocialClient, string, string) (Profile, error) {
			return Profile{OpenID: "gitee-1"}, nil
		},
	}
	state := mustRedirectState(t, svc, 1, 10, "https://app.example.com/callback")
	userID, err := svc.LoginUser(context.Background(), 1, 10, 2, "code", state)
	if err != nil || userID != 8 {
		t.Fatal(userID, err)
	}
}

func TestBindReusesStoredCodeWithoutExchange(t *testing.T) {
	store := &memStore{byCode: &SocialUser{ID: 4, Type: 10, OpenID: "stored"}}
	exchanged := false
	svc := &Service{Store: store, Exchange: func(context.Context, SocialClient, string, string) (Profile, error) {
		exchanged = true
		return Profile{}, nil
	}}
	if err := svc.BindCurrentUser(context.Background(), 1, 7, 10, "code", "state"); err != nil {
		t.Fatal(err)
	}
	if exchanged || store.reboundUser != 7 || store.reboundSocial != 4 || store.reboundType != 10 {
		t.Fatalf("应复用已保存的授权码：exchanged=%v %+v", exchanged, store)
	}
}

func TestUnbindMissingOpenID(t *testing.T) {
	svc := &Service{Store: &memStore{}}
	err := svc.UnbindCurrentUser(context.Background(), 1, 7, 10, "missing")
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_018_001 {
		t.Fatal(err)
	}
}

func TestApproveIssuesCodeRedirect(t *testing.T) {
	store := &memStore{oauth: &OAuthClient{
		ClientID: "default", Name: "芋道", Status: 0,
		AuthorizedGrantTypes: []string{"authorization_code"},
		Scopes:               []string{"user.read"},
		AutoApproveScopes:    []string{"user.read"},
		RedirectURIs:         []string{"https://app.example/cb"},
	}}
	svc := &Service{Store: store}
	link, err := svc.Approve(context.Background(), 1, 7, "code", "default", "https://app.example/cb", "xyz", true, []ScopeChoice{{Key: "user.read", Value: true}})
	if err != nil || link != "https://app.example/cb?code=auth-code&state=xyz" || !store.savedApprove {
		t.Fatalf("%s %v %+v", link, err, store.savedApprove)
	}
	denied, err := svc.Approve(context.Background(), 1, 7, "code", "default", "https://app.example/cb", "xyz", false, []ScopeChoice{{Key: "user.read", Value: false}})
	if err != nil || denied != "https://app.example/cb?error=access_denied&error_description=User+denied+access&state=xyz" {
		t.Fatalf("%s %v", denied, err)
	}
}

func TestIssueTokenConsumesAuthorizationCodeOnce(t *testing.T) {
	store := &memStore{
		oauth: &OAuthClient{
			ClientID: "default", Secret: "secret", Status: 0,
			AuthorizedGrantTypes: []string{"authorization_code"},
			RedirectURIs:         []string{"https://app.example/cb"},
		},
		code: &AuthCode{UserID: 7, UserType: 2, TenantID: 1, ClientID: "default", RedirectURI: "https://app.example/cb", State: "xyz", Scopes: []string{"user.read"}},
	}
	svc := &Service{Store: store, Grants: OpenGrants{Issue: func(context.Context, int64, int64, int, string, []string) (OpenToken, error) {
		return OpenToken{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"user.read"}}, nil
	}}}
	token, err := svc.IssueToken(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: "once", RedirectURI: "https://app.example/cb", State: "xyz",
		ClientID: "default", Secret: "secret",
	})
	if err != nil || token.AccessToken != "access" || token.TokenType != "bearer" || token.Scope != "user.read" {
		t.Fatalf("%+v %v", token, err)
	}
	_, err = svc.IssueToken(context.Background(), TokenRequest{
		GrantType: "authorization_code", Code: "once", RedirectURI: "https://app.example/cb", State: "xyz",
		ClientID: "default", Secret: "secret",
	})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_022_000 {
		t.Fatal(err)
	}
}

type memStore struct {
	clientTaken   bool
	social        *SocialClient
	boundUser     int64
	saved         bool
	byCode        *SocialUser
	byOpenID      *SocialUser
	boundSocial   *SocialUser
	reboundUser   int64
	reboundType   int
	reboundSocial int64
	exchanged     bool
	oauth         *OAuthClient
	approves      []Approve
	savedApprove  bool
	issuedScopes  []string
	code          *AuthCode
}

func (m *memStore) ClientByID(context.Context, int64) (*OAuthClient, error) { return nil, nil }
func (m *memStore) ClientByClientID(context.Context, string) (*OAuthClient, error) {
	return m.oauth, nil
}
func (m *memStore) ClientIDTaken(context.Context, string, int64) (bool, error) {
	return m.clientTaken, nil
}
func (m *memStore) CreateClient(context.Context, OAuthClient) (int64, error) { return 1, nil }
func (m *memStore) UpdateClient(context.Context, OAuthClient) error          { return nil }
func (m *memStore) DeleteClient(context.Context, int64) error                { return nil }
func (m *memStore) SocialByID(context.Context, int64, int64) (*SocialClient, error) {
	return m.social, nil
}
func (m *memStore) SocialByType(context.Context, int64, int, int) (*SocialClient, error) {
	return m.social, nil
}
func (m *memStore) SocialTaken(context.Context, int64, int, int, int64) (bool, error) {
	return false, nil
}
func (m *memStore) CreateSocial(context.Context, int64, SocialClient) (int64, error) {
	return 1, nil
}
func (m *memStore) UpdateSocial(context.Context, int64, SocialClient) error { return nil }
func (m *memStore) DeleteSocial(context.Context, int64, int64) error        { return nil }
func (m *memStore) SocialUserByOpenID(context.Context, int64, int, string) (*SocialUser, error) {
	return m.byOpenID, nil
}
func (m *memStore) SocialUserByCodeState(context.Context, int64, int, string, string) (*SocialUser, error) {
	return m.byCode, nil
}
func (m *memStore) SocialUserByID(context.Context, int64, int64) (*SocialUserDetail, error) {
	return nil, nil
}
func (m *memStore) BindUserID(context.Context, int64, int, string) (int64, bool, error) {
	if m.boundUser == 0 {
		return 0, false, nil
	}
	return m.boundUser, true, nil
}
func (m *memStore) SaveSocialUser(context.Context, int64, SocialUser) (int64, error) {
	m.saved = true
	return 1, nil
}
func (m *memStore) Bind(context.Context, int64, int64, int, int, int64) error { return nil }
func (m *memStore) Rebind(_ context.Context, _, userID int64, _, socialType int, socialUserID int64) error {
	m.reboundUser = userID
	m.reboundType = socialType
	m.reboundSocial = socialUserID
	return nil
}
func (m *memStore) Unbind(context.Context, int64, int64, int, int) error { return nil }
func (m *memStore) SocialByBind(context.Context, int64, int64, int, int) (*SocialUser, error) {
	return m.boundSocial, nil
}
func (m *memStore) BoundUserID(context.Context, int64, int, int64) (*int64, error) { return nil, nil }
func (m *memStore) Approves(context.Context, int64, int64, int, string, time.Time) ([]Approve, error) {
	return m.approves, nil
}
func (m *memStore) SaveApprove(context.Context, int64, int64, int, string, string, bool, time.Time) error {
	m.savedApprove = true
	return nil
}
func (m *memStore) InsertCode(_ context.Context, _, _ int64, _ int, _ string, scopes []string, _, _ string, _ time.Time) (string, error) {
	m.issuedScopes = scopes
	return "auth-code", nil
}
func (m *memStore) ConsumeCode(context.Context, string, time.Time) (*AuthCode, error) {
	if m.code == nil {
		return nil, &Error{Code: 1_002_022_000, Msg: "code 不存在"}
	}
	item := *m.code
	m.code = nil
	return &item, nil
}
