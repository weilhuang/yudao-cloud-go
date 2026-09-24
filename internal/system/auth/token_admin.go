package auth

import (
	"context"
	"strings"
	"time"
)

// AccessTokenQuery 是管理端令牌分页的筛选条件。空指针表示前端没有传该条件。
type AccessTokenQuery struct {
	PageNo   int
	PageSize int
	UserID   *int64
	UserType *int
	ClientID string
}

// AccessTokenItem 对齐 OAuth2AccessTokenRespVO。时间是毫秒时间戳。
type AccessTokenItem struct {
	ID           int64  `json:"id"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	UserID       int64  `json:"userId"`
	UserType     int    `json:"userType"`
	ClientID     string `json:"clientId"`
	CreateTime   int64  `json:"createTime"`
	ExpiresTime  int64  `json:"expiresTime"`
}

// AccessTokenPage 是管理端令牌分页。
type AccessTokenPage struct {
	List  []AccessTokenItem `json:"list"`
	Total int64             `json:"total"`
}

// AccessTokenPage 只返回当前租户尚未过期的访问令牌。
func (s *Service) AccessTokenPage(ctx context.Context, tenantID int64, query AccessTokenQuery) (AccessTokenPage, error) {
	page, err := s.Tokens.AccessPage(ctx, tenantID, query, s.now())
	if err != nil {
		return AccessTokenPage{}, err
	}
	if page.List == nil {
		page.List = []AccessTokenItem{}
	}
	return page, nil
}

func accessPageBounds(query AccessTokenQuery) (int, int) {
	pageNo, pageSize := query.PageNo, query.PageSize
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return pageNo, pageSize
}

func tokenVisible(token Token, tenantID int64, query AccessTokenQuery, now time.Time) bool {
	if token.TenantID != tenantID || !token.ExpiresAt.After(now) {
		return false
	}
	if query.UserID != nil && token.UserID != *query.UserID {
		return false
	}
	if query.UserType != nil && token.UserType != *query.UserType {
		return false
	}
	if query.ClientID != "" && !strings.Contains(token.ClientID, query.ClientID) {
		return false
	}
	return true
}
