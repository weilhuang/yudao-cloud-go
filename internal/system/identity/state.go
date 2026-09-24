package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	socialStateTTL    = 5 * time.Minute
	socialStatePrefix = "social:state:"
)

// StateStore 保存短时授权记录。GetDel 必须原子读取并删除，才能阻止并发重放。
type StateStore interface {
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	GetDel(ctx context.Context, key string) (string, error)
}

// RedisStateStore 在多个 Go 实例之间共享授权记录。
type RedisStateStore struct {
	Client *redis.Client
}

func (r RedisStateStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.Client.Set(ctx, key, value, ttl).Err()
}

func (r RedisStateStore) GetDel(ctx context.Context, key string) (string, error) {
	value, err := r.Client.GetDel(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return value, err
}

// socialState 绑定发起授权时的租户、平台、用户类型和回调地址。
// 回调请求只带 state；换码时不接受调用者另给的 redirectUri。
type socialState struct {
	TenantID    int64  `json:"tenantId"`
	SocialType  int    `json:"socialType"`
	UserType    int    `json:"userType"`
	RedirectURI string `json:"redirectUri"`
}

func stateKey(state string) string { return socialStatePrefix + state }

// consumeState 在访问社交平台前领取 state。即使平台换码失败，也不能重试这张票据。
func (s *Service) consumeState(ctx context.Context, tenantID int64, socialType, userType int, state string) (string, error) {
	if len(state) != 64 {
		return "", invalidSocialState()
	}
	if _, err := hex.DecodeString(state); err != nil {
		return "", invalidSocialState()
	}
	if s.StateStore == nil {
		return "", &Error{Code: 1_002_018_000, Msg: "社交授权失败，原因是：state 存储未配置"}
	}
	raw, err := s.StateStore.GetDel(ctx, stateKey(state))
	if err != nil {
		return "", err
	}
	var saved socialState
	if raw == "" || json.Unmarshal([]byte(raw), &saved) != nil || saved.RedirectURI == "" ||
		saved.TenantID != tenantID || saved.SocialType != socialType || saved.UserType != userType {
		return "", invalidSocialState()
	}
	return saved.RedirectURI, nil
}

func invalidSocialState() *Error {
	return &Error{Code: 1_002_018_000, Msg: "社交授权失败，原因是：state 无效或已过期"}
}
