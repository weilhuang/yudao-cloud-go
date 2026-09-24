package auth

import "context"

// CreateAccessToken 为 Java Feign 客户端创建令牌。管理员与会员都写入同一套令牌表，
// 但会员不会查询管理员表；管理端 Session 会另外拒绝会员令牌。
func (s *Service) CreateAccessToken(ctx context.Context, tenantID, userID int64, userType int, clientID string, scopes []string) (*Token, error) {
	if userType != userTypeAdmin && userType != userTypeMember {
		return nil, badRequest("用户类型不正确")
	}
	if err := s.checkTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	client, err := s.clientByID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	var user *User
	if userType == userTypeAdmin && userID > 0 {
		user, err = s.Users.FindByID(ctx, userID)
		if err != nil {
			return nil, err
		}
		if user == nil || user.TenantID != tenantID || user.Status != statusEnable {
			return nil, unauthorized("账号不存在或已被禁用")
		}
	}
	refresh := Token{
		RefreshToken: newToken(),
		UserID:       userID,
		UserType:     userType,
		TenantID:     tenantID,
		ClientID:     client.ClientID,
		Scopes:       scopes,
		ExpiresAt:    s.now().Add(client.RefreshTTL),
	}
	return s.issuePair(ctx, refresh, user, client)
}

// RefreshTokenRPC 按 Feign 传入的 clientId 校验刷新令牌归属，防止跨客户端使用。
func (s *Service) RefreshTokenRPC(ctx context.Context, refreshToken, clientID string, tenantID int64) (*Token, error) {
	if tenantID >= 0 {
		if err := s.checkTenant(ctx, tenantID); err != nil {
			return nil, err
		}
	}
	return s.refreshToken(ctx, refreshToken, clientID, tenantID)
}

// RemoveAccessToken 与管理端注销共用令牌存储，但不生成管理员注销日志。
// Java 在令牌不存在时返回 data:null；这里同样保持幂等。
func (s *Service) RemoveAccessToken(ctx context.Context, tenantID int64, accessToken string) (*Token, error) {
	token, err := s.Tokens.FindAccess(ctx, accessToken)
	if err != nil || token == nil {
		return token, err
	}
	if token.TenantID != tenantID {
		return nil, forbidden("您无权访问该租户的数据")
	}
	if _, err := s.Tokens.DeleteAccess(ctx, accessToken); err != nil {
		return nil, err
	}
	if err := s.Tokens.DeleteRefresh(ctx, token.RefreshToken); err != nil {
		return nil, err
	}
	// Java 也会尝试删除刷新令牌对应的 Redis key；Go 的持久状态由 MySQL 决定。
	if err := s.Cache.Delete(ctx, accessToken, token.RefreshToken); err != nil {
		return nil, err
	}
	return token, nil
}

// RevokeAdminTokens 撤销指定管理员在当前租户的访问令牌和刷新令牌。
// 用户类型固定为管理员，对齐 Java 禁用用户时的 removeAccessToken(userId, ADMIN)。
func (s *Service) RevokeAdminTokens(ctx context.Context, tenantID, userID int64) error {
	return s.RemoveAccessTokensByUser(ctx, tenantID, userID, userTypeAdmin)
}

// RemoveAccessTokensByUser 仅撤销当前租户的指定用户类型令牌。
func (s *Service) RemoveAccessTokensByUser(ctx context.Context, tenantID, userID int64, userType int) error {
	tokens, err := s.Tokens.AccessesByUser(ctx, tenantID, userID, userType)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if _, err := s.RemoveAccessToken(ctx, tenantID, token.AccessToken); err != nil {
			return err
		}
	}
	return nil
}
