package identity

import (
	"context"
	"strings"
	"time"
)

// TokenRequest 是开放令牌接口的参数。
type TokenRequest struct {
	GrantType   string
	Code        string
	RedirectURI string
	State       string
	Username    string
	Password    string
	Scope       string
	Refresh     string
	ClientID    string
	Secret      string
	TenantID    int64
}

// AccessTokenResponse 对齐开放接口的 access_token 字段名。
type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}

// CheckTokenResponse 对齐开放接口的校验结果。exp 是秒级时间戳。
type CheckTokenResponse struct {
	UserID      int64    `json:"user_id"`
	UserType    int      `json:"user_type"`
	TenantID    int64    `json:"tenant_id"`
	ClientID    string   `json:"client_id"`
	Scopes      []string `json:"scopes"`
	AccessToken string   `json:"access_token"`
	Exp         int64    `json:"exp"`
}

// IssueToken 按授权类型签发访问令牌。implicit 不能走这个接口。
func (s *Service) IssueToken(ctx context.Context, req TokenRequest) (*AccessTokenResponse, error) {
	if req.GrantType == "implicit" {
		return nil, &Error{Code: 400, Msg: "Token 接口不支持 implicit 授权模式"}
	}
	grant, ok := openGrant(req.GrantType)
	if !ok {
		return nil, &Error{Code: 400, Msg: "未知授权类型(" + req.GrantType + ")"}
	}
	scopes := splitScope(req.Scope)
	client, err := s.validClient(ctx, req.ClientID, req.Secret, true, grant, scopes, req.RedirectURI)
	if err != nil {
		return nil, err
	}
	var token OpenToken
	switch grant {
	case "authorization_code":
		code, err := s.Store.ConsumeCode(ctx, req.Code, time.Now())
		if err != nil {
			return nil, err
		}
		if code.ClientID != client.ClientID {
			return nil, &Error{Code: 1_002_021_000, Msg: "client_id 不匹配"}
		}
		if code.RedirectURI != req.RedirectURI {
			return nil, &Error{Code: 1_002_021_001, Msg: "redirect_uri 不匹配"}
		}
		state := req.State
		if code.State != state {
			return nil, &Error{Code: 1_002_021_002, Msg: "state 不匹配"}
		}
		if s.Grants.Issue == nil {
			return nil, &Error{Code: 500, Msg: "系统异常"}
		}
		token, err = s.Grants.Issue(ctx, code.TenantID, code.UserID, code.UserType, client.ClientID, code.Scopes)
		if err != nil {
			return nil, err
		}
	case "password":
		if s.Grants.Password == nil {
			return nil, &Error{Code: 500, Msg: "系统异常"}
		}
		token, err = s.Grants.Password(ctx, req.TenantID, req.Username, req.Password, client.ClientID, scopes)
		if err != nil {
			return nil, err
		}
	case "client_credentials":
		if s.Grants.Issue == nil {
			return nil, &Error{Code: 500, Msg: "系统异常"}
		}
		token, err = s.Grants.Issue(ctx, req.TenantID, 0, userTypeAdmin, client.ClientID, scopes)
		if err != nil {
			return nil, err
		}
	case "refresh_token":
		if s.Grants.Refresh == nil {
			return nil, &Error{Code: 500, Msg: "系统异常"}
		}
		token, err = s.Grants.Refresh(ctx, req.Refresh, client.ClientID)
		if err != nil {
			return nil, err
		}
	default:
		return nil, &Error{Code: 400, Msg: "未知授权类型(" + req.GrantType + ")"}
	}
	seconds := int64(time.Until(token.ExpiresAt).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return &AccessTokenResponse{
		AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		TokenType: "bearer", ExpiresIn: seconds, Scope: strings.Join(token.Scopes, " "),
	}, nil
}

// CheckOpenToken 用客户端密钥校验访问令牌。
func (s *Service) CheckOpenToken(ctx context.Context, clientID, secret, access string) (*CheckTokenResponse, error) {
	if _, err := s.validClient(ctx, clientID, secret, true, "", nil, ""); err != nil {
		return nil, err
	}
	if s.Grants.Check == nil {
		return nil, &Error{Code: 500, Msg: "系统异常"}
	}
	token, err := s.Grants.Check(ctx, access)
	if err != nil {
		return nil, err
	}
	return &CheckTokenResponse{
		UserID: token.UserID, UserType: token.UserType, TenantID: token.TenantID,
		ClientID: token.ClientID, Scopes: token.Scopes, AccessToken: token.AccessToken,
		Exp: token.ExpiresAt.Unix(),
	}, nil
}

// RevokeOpenToken 只删除属于该客户端的令牌。不匹配或不存在时返回 false。
func (s *Service) RevokeOpenToken(ctx context.Context, clientID, secret, access string) (bool, error) {
	if _, err := s.validClient(ctx, clientID, secret, true, "", nil, ""); err != nil {
		return false, err
	}
	if s.Grants.Revoke == nil {
		return false, &Error{Code: 500, Msg: "系统异常"}
	}
	return s.Grants.Revoke(ctx, clientID, access)
}

func openGrant(grant string) (string, bool) {
	switch grant {
	case "authorization_code", "password", "client_credentials", "refresh_token":
		return grant, true
	default:
		return "", false
	}
}

func splitScope(scope string) []string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	return strings.Fields(scope)
}
