package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache 用和 Java 相同的键保存访问令牌，TTL 对齐过期时间。
type RedisCache struct {
	Client *redis.Client
}

// redisAccessToken 对齐 Java OAuth2AccessTokenDO 的 JSON 字段及毫秒时间戳。
// 只保存令牌字段；Java 在写缓存前也会清空审计字段。
type redisAccessToken struct {
	AccessToken  string            `json:"accessToken"`
	RefreshToken string            `json:"refreshToken"`
	UserID       int64             `json:"userId"`
	UserType     int               `json:"userType"`
	TenantID     int64             `json:"tenantId"`
	ClientID     string            `json:"clientId"`
	Scopes       []string          `json:"scopes"`
	ExpiresTime  int64             `json:"expiresTime"`
	UserInfo     map[string]string `json:"userInfo"`
}

func (c *RedisCache) Put(ctx context.Context, accessToken string, token Token) error {
	raw, err := json.Marshal(redisAccessToken{
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		UserID: token.UserID, UserType: token.UserType, TenantID: token.TenantID,
		ClientID: token.ClientID, Scopes: token.Scopes,
		ExpiresTime: token.ExpiresAt.UnixMilli(), UserInfo: token.UserInfo,
	})
	if err != nil {
		return err
	}
	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 {
		return nil
	}
	return c.Client.Set(ctx, tokenKey(accessToken), raw, ttl).Err()
}

func (c *RedisCache) Get(ctx context.Context, accessToken string) (*Token, error) {
	raw, err := c.Client.Get(ctx, tokenKey(accessToken)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cached redisAccessToken
	if err := json.Unmarshal(raw, &cached); err != nil {
		return nil, err
	}
	if cached.ExpiresTime > 0 {
		return &Token{
			AccessToken: cached.AccessToken, RefreshToken: cached.RefreshToken,
			UserID: cached.UserID, UserType: cached.UserType, TenantID: cached.TenantID,
			ClientID: cached.ClientID, Scopes: cached.Scopes,
			ExpiresAt: time.UnixMilli(cached.ExpiresTime), UserInfo: cached.UserInfo,
		}, nil
	}
	// 旧 Go 版本用大写字段和 RFC3339 时间；过渡期仍可读取旧缓存。
	var legacy Token
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return nil, err
	}
	if legacy.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("缓存令牌缺少过期时间")
	}
	return &legacy, nil
}

func (c *RedisCache) Delete(ctx context.Context, accessTokens ...string) error {
	if len(accessTokens) == 0 {
		return nil
	}
	keys := make([]string, len(accessTokens))
	for i, token := range accessTokens {
		keys[i] = tokenKey(token)
	}
	return c.Client.Del(ctx, keys...).Err()
}

func tokenKey(accessToken string) string {
	return fmt.Sprintf("oauth2_access_token:%s", accessToken)
}
