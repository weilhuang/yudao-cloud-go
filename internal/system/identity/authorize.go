package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	grantAuthorizationCode = "authorization_code"
	grantImplicit          = "implicit"
	approveTTL             = 30 * 24 * time.Hour
	codeTTL                = 5 * time.Minute
	userTypeAdmin          = 2
)

// ScopeChoice 是授权页上的一项范围，以及用户是否同意。
type ScopeChoice struct {
	Key   string `json:"key"`
	Value bool   `json:"value"`
}

// AuthorizeInfo 对齐 OAuth2OpenAuthorizeInfoRespVO。
type AuthorizeInfo struct {
	Client struct {
		Name string `json:"name"`
		Logo string `json:"logo"`
	} `json:"client"`
	Scopes []ScopeChoice `json:"scopes"`
}

// IssueAccess 给简化模式签发访问令牌。由应用装配接到登录服务。
type IssueAccess func(ctx context.Context, tenantID, userID int64, clientID string, scopes []string) (token string, expires time.Time, err error)

// AuthorizeInfo 返回客户端名称、图标，以及当前用户是否已经同意每个范围。
func (s *Service) AuthorizeInfo(ctx context.Context, tenantID, userID int64, clientID string) (*AuthorizeInfo, error) {
	client, err := s.validClient(ctx, clientID, "", false, "", nil, "")
	if err != nil {
		return nil, err
	}
	approves, err := s.Store.Approves(ctx, tenantID, userID, userTypeAdmin, clientID, time.Now())
	if err != nil {
		return nil, err
	}
	approved := map[string]bool{}
	for _, item := range approves {
		approved[item.Scope] = item.Approved
	}
	info := &AuthorizeInfo{}
	info.Client.Name = client.Name
	info.Client.Logo = client.Logo
	info.Scopes = make([]ScopeChoice, 0, len(client.Scopes))
	for _, scope := range client.Scopes {
		info.Scopes = append(info.Scopes, ScopeChoice{Key: scope, Value: approved[scope]})
	}
	return info, nil
}

// Approve 处理自动授权和手动确认，返回前端应跳转的地址。自动授权未通过时返回空字符串。
func (s *Service) Approve(ctx context.Context, tenantID, userID int64, responseType, clientID, redirectURI, state string, auto bool, scopes []ScopeChoice) (string, error) {
	grant, err := grantType(responseType)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(scopes))
	for _, item := range scopes {
		keys = append(keys, item.Key)
	}
	client, err := s.validClient(ctx, clientID, "", false, grant, keys, redirectURI)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if auto {
		ok, err := s.preApprove(ctx, tenantID, userID, client, keys, now)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", nil
		}
	} else if !approvedAny(scopes) && len(scopes) > 0 {
		if err := s.saveChoices(ctx, tenantID, userID, clientID, scopes, now); err != nil {
			return "", err
		}
		return unsuccessfulRedirect(redirectURI, responseType, state), nil
	} else if err := s.saveChoices(ctx, tenantID, userID, clientID, scopes, now); err != nil {
		return "", err
	}
	accepted := acceptedScopes(scopes, auto)
	if grant == grantAuthorizationCode {
		code, err := s.Store.InsertCode(ctx, tenantID, userID, userTypeAdmin, client.ClientID, accepted, redirectURI, state, now.Add(codeTTL))
		if err != nil {
			return "", err
		}
		return codeRedirect(redirectURI, code, state), nil
	}
	if s.IssueAccess == nil {
		return "", &Error{Code: 500, Msg: "系统异常"}
	}
	token, expires, err := s.IssueAccess(ctx, tenantID, userID, client.ClientID, accepted)
	if err != nil {
		return "", err
	}
	return implicitRedirect(redirectURI, token, state, expires, accepted, client.AdditionalInformation), nil
}

func (s *Service) preApprove(ctx context.Context, tenantID, userID int64, client *OAuthClient, scopes []string, now time.Time) (bool, error) {
	if containsAll(client.AutoApproveScopes, scopes) {
		for _, scope := range scopes {
			if err := s.Store.SaveApprove(ctx, tenantID, userID, userTypeAdmin, client.ClientID, scope, true, now.Add(approveTTL)); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	approves, err := s.Store.Approves(ctx, tenantID, userID, userTypeAdmin, client.ClientID, now)
	if err != nil {
		return false, err
	}
	have := make([]string, 0, len(approves))
	for _, item := range approves {
		if item.Approved {
			have = append(have, item.Scope)
		}
	}
	return containsAll(have, scopes), nil
}

func (s *Service) saveChoices(ctx context.Context, tenantID, userID int64, clientID string, scopes []ScopeChoice, now time.Time) error {
	expire := now.Add(approveTTL)
	for _, item := range scopes {
		if err := s.Store.SaveApprove(ctx, tenantID, userID, userTypeAdmin, clientID, item.Key, item.Value, expire); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validClient(ctx context.Context, clientID, secret string, checkSecret bool, grant string, scopes []string, redirectURI string) (*OAuthClient, error) {
	client, err := s.Store.ClientByClientID(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, &Error{Code: 1_002_020_000, Msg: "OAuth2 客户端不存在"}
	}
	if client.Status != 0 {
		return nil, &Error{Code: 1_002_020_002, Msg: "OAuth2 客户端已禁用"}
	}
	if checkSecret && secret != client.Secret {
		return nil, &Error{Code: 1_002_020_006, Msg: fmt.Sprintf("无效 client_secret: %s", secret)}
	}
	if grant != "" && !listHas(client.AuthorizedGrantTypes, grant) {
		return nil, &Error{Code: 1_002_020_003, Msg: "不支持该授权类型"}
	}
	if len(scopes) > 0 && !containsAll(client.Scopes, scopes) {
		return nil, &Error{Code: 1_002_020_004, Msg: "授权范围过大"}
	}
	if redirectURI != "" && !hasPrefixAny(redirectURI, client.RedirectURIs) {
		return nil, &Error{Code: 1_002_020_005, Msg: fmt.Sprintf("无效 redirect_uri: %s", redirectURI)}
	}
	return client, nil
}

func grantType(responseType string) (string, error) {
	switch responseType {
	case "code":
		return grantAuthorizationCode, nil
	case "token":
		return grantImplicit, nil
	default:
		return "", &Error{Code: 400, Msg: "response_type 参数值只允许 code 和 token"}
	}
}

func acceptedScopes(scopes []ScopeChoice, auto bool) []string {
	out := make([]string, 0, len(scopes))
	for _, item := range scopes {
		if auto || item.Value {
			out = append(out, item.Key)
		}
	}
	return out
}

func approvedAny(scopes []ScopeChoice) bool {
	for _, item := range scopes {
		if item.Value {
			return true
		}
	}
	return false
}

func listHas(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func containsAll(have, want []string) bool {
	for _, item := range want {
		if !listHas(have, item) {
			return false
		}
	}
	return true
}

func hasPrefixAny(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func randomCode() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func codeRedirect(redirectURI, code, state string) string {
	pairs := [][2]string{{"code", code}}
	if state != "" {
		pairs = append(pairs, [2]string{"state", state})
	}
	return appendParams(redirectURI, pairs, false)
}

func unsuccessfulRedirect(redirectURI, responseType, state string) string {
	pairs := [][2]string{{"error", "access_denied"}, {"error_description", "User denied access"}}
	if state != "" {
		pairs = append(pairs, [2]string{"state", state})
	}
	return appendParams(redirectURI, pairs, !strings.Contains(responseType, "code"))
}

func implicitRedirect(redirectURI, token, state string, expires time.Time, scopes []string, extra string) string {
	pairs := [][2]string{{"access_token", token}, {"token_type", "bearer"}}
	if state != "" {
		pairs = append(pairs, [2]string{"state", state})
	}
	if !expires.IsZero() {
		seconds := int(time.Until(expires).Seconds())
		if seconds < 0 {
			seconds = 0
		}
		pairs = append(pairs, [2]string{"expires_in", fmt.Sprintf("%d", seconds)})
	}
	if len(scopes) > 0 {
		pairs = append(pairs, [2]string{"scope", strings.Join(scopes, " ")})
	}
	var additional map[string]any
	if json.Unmarshal([]byte(extra), &additional) == nil {
		for key, value := range additional {
			if value == nil {
				continue
			}
			pairs = append(pairs, [2]string{"extra_" + key, fmt.Sprint(value)})
		}
	}
	return appendParams(redirectURI, pairs, true)
}

func appendParams(base string, pairs [][2]string, fragment bool) string {
	var b strings.Builder
	for i, pair := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(pair[0]))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(pair[1]))
	}
	encoded := b.String()
	if fragment {
		if strings.Contains(base, "#") {
			return base + "&" + encoded
		}
		return base + "#" + encoded
	}
	if strings.Contains(base, "?") {
		return base + "&" + encoded
	}
	return base + "?" + encoded
}

func parseScopes(raw string) ([]ScopeChoice, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	token, err := dec.Token()
	if err != nil {
		return nil, &Error{Code: 400, Msg: "请求参数不正确"}
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, &Error{Code: 400, Msg: "请求参数不正确"}
	}
	var out []ScopeChoice
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		var approved bool
		if err := dec.Decode(&approved); err != nil {
			return nil, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		out = append(out, ScopeChoice{Key: key, Value: approved})
	}
	return out, nil
}
