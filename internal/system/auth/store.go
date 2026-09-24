package auth

import (
	"context"
	"time"
)

// UserStore 按租户查用户。tenantID 为 0 表示这次查询忽略租户，对应登录接口未带 tenant-id。
type UserStore interface {
	FindByUsername(ctx context.Context, tenantID int64, username string) (*User, error)
	FindByMobile(ctx context.Context, tenantID int64, mobile string) (*User, error)
	FindByID(ctx context.Context, id int64) (*User, error)
	TouchLogin(ctx context.Context, id int64, ip string, at int64) error
	// CountUsers 与 AccountLimit 给注册接口核对租户账号配额。
	CountUsers(ctx context.Context, tenantID int64) (int64, error)
	AccountLimit(ctx context.Context, tenantID int64) (int64, error)
	ConfigValue(ctx context.Context, key string) (string, error)
	RegisterUser(ctx context.Context, tenantID int64, username, nickname, passwordHash string) (*User, error)
	UpdatePassword(ctx context.Context, tenantID, userID int64, passwordHash string) error
}

// TokenStore 读写令牌和默认客户端。Redis 未命中时回源 MySQL。
type TokenStore interface {
	Client(ctx context.Context, clientID string) (*Client, error)
	InsertAccess(ctx context.Context, token Token) error
	// InsertPair 在同一事务写入刷新令牌和访问令牌。
	InsertPair(ctx context.Context, refresh, access Token) error
	FindAccess(ctx context.Context, accessToken string) (*Token, error)
	FindRefresh(ctx context.Context, refreshToken string) (*Token, error)
	DeleteAccessByRefresh(ctx context.Context, refreshToken string) ([]string, error)
	DeleteRefresh(ctx context.Context, refreshToken string) error
	DeleteAccess(ctx context.Context, accessToken string) (*Token, error)
	// AccessesByUser 只返回指定租户、用户和用户类型的有效访问令牌。
	AccessesByUser(ctx context.Context, tenantID, userID int64, userType int) ([]Token, error)
	// AccessPage 返回当前租户尚未过期的访问令牌。
	AccessPage(ctx context.Context, tenantID int64, query AccessTokenQuery, now time.Time) (AccessTokenPage, error)
}

// PermissionStore 读取角色和菜单。超级管理员在用例里展开成全部菜单。
type PermissionStore interface {
	RolesByUser(ctx context.Context, userID int64) ([]Role, error)
	MenusByRole(ctx context.Context, roleIDs []int64, all bool) ([]Menu, error)
}

// TokenCache 的键和值均与 Java OAuth2AccessTokenRedisDAO 对齐。
type TokenCache interface {
	Put(ctx context.Context, accessToken string, token Token) error
	Get(ctx context.Context, accessToken string) (*Token, error)
	Delete(ctx context.Context, accessTokens ...string) error
}
