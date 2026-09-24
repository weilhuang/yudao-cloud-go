package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type stateEntry struct {
	value   string
	expires time.Time
}

// memStateStore 用一把锁模拟 Redis GETDEL 的原子性，并允许测试推进时钟。
type memStateStore struct {
	mu      sync.Mutex
	now     time.Time
	entries map[string]stateEntry
}

func newMemStateStore() *memStateStore {
	return &memStateStore{now: time.Now(), entries: make(map[string]stateEntry)}
}

func (m *memStateStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = stateEntry{value: value, expires: m.now.Add(ttl)}
	return nil
}

func (m *memStateStore) GetDel(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	delete(m.entries, key)
	if !ok || !m.now.Before(entry.expires) {
		return "", nil
	}
	return entry.value, nil
}

func (m *memStateStore) advance(delta time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(delta)
}

func mustRedirectState(t *testing.T, svc *Service, tenantID int64, socialType int, redirectURI string) string {
	t.Helper()
	link, err := svc.Redirect(context.Background(), tenantID, socialType, 2, redirectURI)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if len(state) != 64 {
		t.Fatalf("state 长度错误：%q", state)
	}
	return state
}

func stateTestService(cache StateStore, exchange Exchanger) *Service {
	return &Service{
		Store:      &memStore{social: &SocialClient{SocialType: 10, UserType: 2, Status: 0, ClientID: "gitee"}, boundUser: 8},
		StateStore: cache,
		Exchange:   exchange,
	}
}

func requireInvalidState(t *testing.T, err error) {
	t.Helper()
	biz, ok := err.(*Error)
	if !ok || biz.Code != 1_002_018_000 || !strings.Contains(biz.Msg, "state") {
		t.Fatalf("应拒绝无效 state，实际 %v", err)
	}
}

func TestSocialStateRejectsForgedMissingAndExpired(t *testing.T) {
	cache := newMemStateStore()
	var calls atomic.Int32
	svc := stateTestService(cache, func(context.Context, SocialClient, string, string) (Profile, error) {
		calls.Add(1)
		return Profile{OpenID: "gitee-1"}, nil
	})
	for _, state := range []string{"", strings.Repeat("f", 64), "abc"} {
		_, err := svc.LoginUser(context.Background(), 1, 10, 2, "code", state)
		if state == "" {
			biz, ok := err.(*Error)
			if !ok || biz.Code != 400 {
				t.Fatalf("空 state 应报参数错误：%v", err)
			}
		} else {
			requireInvalidState(t, err)
		}
	}
	state := mustRedirectState(t, svc, 1, 10, "https://app.example.com/callback")
	cache.advance(socialStateTTL)
	_, err := svc.LoginUser(context.Background(), 1, 10, 2, "code", state)
	requireInvalidState(t, err)
	if calls.Load() != 0 {
		t.Fatalf("无效 state 仍调用了第三方：%d", calls.Load())
	}
}

func TestSocialStateBindsTenantPlatformAndUserType(t *testing.T) {
	for _, test := range []struct {
		name       string
		tenantID   int64
		socialType int
		userType   int
	}{
		{name: "跨租户", tenantID: 2, socialType: 10, userType: 2},
		{name: "跨平台", tenantID: 1, socialType: 20, userType: 2},
		{name: "跨用户类型", tenantID: 1, socialType: 10, userType: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			svc := stateTestService(newMemStateStore(), func(context.Context, SocialClient, string, string) (Profile, error) {
				calls.Add(1)
				return Profile{OpenID: "gitee-1"}, nil
			})
			state := mustRedirectState(t, svc, 1, 10, "https://app.example.com/callback")
			_, err := svc.LoginUser(context.Background(), test.tenantID, test.socialType, test.userType, "code", state)
			requireInvalidState(t, err)
			// 尝试错配后，合法调用也不能重新使用同一张票据。
			_, err = svc.LoginUser(context.Background(), 1, 10, 2, "code", state)
			requireInvalidState(t, err)
			if calls.Load() != 0 {
				t.Fatalf("错配 state 仍调用了第三方：%d", calls.Load())
			}
		})
	}
}

func TestSocialStateConcurrentReplayOnlyOneSucceeds(t *testing.T) {
	var calls atomic.Int32
	svc := stateTestService(newMemStateStore(), func(context.Context, SocialClient, string, string) (Profile, error) {
		calls.Add(1)
		return Profile{OpenID: "gitee-1"}, nil
	})
	state := mustRedirectState(t, svc, 1, 10, "https://app.example.com/callback")
	const workers = 32
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := svc.LoginUser(context.Background(), 1, 10, 2, "code", state)
			if err == nil && id != 8 {
				err = fmt.Errorf("用户编号错误：%d", id)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			requireInvalidState(t, err)
		}
	}
	if successes != 1 || calls.Load() != 1 {
		t.Fatalf("并发重放：成功 %d 次，换码 %d 次", successes, calls.Load())
	}
}

func TestSocialCallbackCannotReplaceRedirectURI(t *testing.T) {
	const original = "https://app.example.com/auth/social-login?type=10"
	var exchangedURI string
	svc := stateTestService(newMemStateStore(), func(_ context.Context, _ SocialClient, _, redirectURI string) (Profile, error) {
		exchangedURI = redirectURI
		return Profile{OpenID: "gitee-1"}, nil
	})
	// 未绑定时 handler 在签发令牌前返回业务错误，便于只测回调参数。
	svc.Store.(*memStore).boundUser = 0
	state := mustRedirectState(t, svc, 1, 10, original)
	engine := gin.New()
	h := &handler{svc: svc}
	engine.POST("/social-login", h.socialLogin)
	body := fmt.Sprintf(`{"type":10,"code":"code","state":%q,"redirectUri":"https://attacker.example/callback"}`, state)
	req := httptest.NewRequest(http.MethodPost, "/social-login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("tenant-id", "1")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	var result struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != 1_002_018_001 || exchangedURI != original {
		t.Fatalf("回调地址被替换：code=%d, 换码地址=%q", result.Code, exchangedURI)
	}
}

func TestSocialRedirectIgnoresCallerState(t *testing.T) {
	svc := stateTestService(newMemStateStore(), nil)
	engine := gin.New()
	h := &handler{svc: svc}
	engine.GET("/social-auth-redirect", h.redirect)
	req := httptest.NewRequest(http.MethodGet, "/social-auth-redirect?type=10&redirectUri=https%3A%2F%2Fapp.example.com%2Fcallback&state=attacker", nil)
	req.Header.Set("tenant-id", "1")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	var result struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(result.Data)
	if err != nil || result.Code != 0 || len(link.Query().Get("state")) != 64 || link.Query().Get("state") == "attacker" {
		t.Fatalf("未生成服务端 state：%s", w.Body.String())
	}
}
