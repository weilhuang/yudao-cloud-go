// Package identity 提供 OAuth2 客户端和社交登录配置。
package identity

// OAuthClient 是 system_oauth2_client。数组字段在库里存 JSON。
type OAuthClient struct {
	ID                          int64    `json:"id"`
	ClientID                    string   `json:"clientId"`
	Secret                      string   `json:"secret"`
	Name                        string   `json:"name"`
	Logo                        string   `json:"logo"`
	Description                 string   `json:"description"`
	Status                      int      `json:"status"`
	AccessTokenValiditySeconds  int      `json:"accessTokenValiditySeconds"`
	RefreshTokenValiditySeconds int      `json:"refreshTokenValiditySeconds"`
	RedirectURIs                []string `json:"redirectUris"`
	AuthorizedGrantTypes        []string `json:"authorizedGrantTypes"`
	Scopes                      []string `json:"scopes"`
	AutoApproveScopes           []string `json:"autoApproveScopes"`
	Authorities                 []string `json:"authorities"`
	ResourceIDs                 []string `json:"resourceIds"`
	AdditionalInformation       string   `json:"additionalInformation"`
	CreateTime                  int64    `json:"createTime,omitempty"`
}

// SocialClient 是社交登录的应用配置。同一租户里，同一用户类型的同一社交平台只能有一条。
type SocialClient struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	SocialType   int    `json:"socialType"`
	UserType     int    `json:"userType"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	AgentID      string `json:"agentId"`
	PublicKey    string `json:"publicKey"`
	Status       int    `json:"status"`
	CreateTime   int64  `json:"createTime,omitempty"`
}

// SocialUser 是已绑定的社交账号。
type SocialUser struct {
	ID         int64  `json:"id"`
	Type       int    `json:"type"`
	OpenID     string `json:"openid"`
	Nickname   string `json:"nickname"`
	Avatar     string `json:"avatar"`
	UserID     int64  `json:"userId,omitempty"`
	CreateTime int64  `json:"createTime,omitempty"`
	Code       string `json:"code,omitempty"`
	State      string `json:"state,omitempty"`
}

// SocialUserDetail 是管理端详情。字段对齐 SocialUserRespVO。
type SocialUserDetail struct {
	ID           int64  `json:"id"`
	Type         int    `json:"type"`
	OpenID       string `json:"openid"`
	Token        string `json:"token"`
	RawTokenInfo string `json:"rawTokenInfo"`
	Nickname     string `json:"nickname"`
	Avatar       string `json:"avatar"`
	RawUserInfo  string `json:"rawUserInfo"`
	Code         string `json:"code"`
	State        string `json:"state"`
	CreateTime   int64  `json:"createTime"`
	UpdateTime   int64  `json:"updateTime"`
}

// Page 是管理后台分页。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// Error 是身份配置的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
