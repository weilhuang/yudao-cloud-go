package auth

import "time"

const (
	userTypeMember  = 1
	userTypeAdmin   = 2
	clientIDDefault = "default"
	statusEnable    = 0
	menuDir         = 1
	menuMenu        = 2
	menuButton      = 3
	roleSuperAdmin  = "super_admin"
)

// User 是登录和权限信息要用到的管理员字段。
type User struct {
	ID       int64
	TenantID int64
	Username string
	Password string
	Nickname string
	Avatar   string
	Email    string
	DeptID   *int64
	Status   int
}

// Client 是 OAuth2 客户端上的令牌有效期。默认客户端 client_id 是 default。
type Client struct {
	ClientID   string
	Status     int
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// Token 是访问令牌。过期时间用本地时区，JSON 输出毫秒时间戳。
type Token struct {
	ID           int64
	AccessToken  string
	RefreshToken string
	UserID       int64
	UserType     int
	TenantID     int64
	ClientID     string
	Scopes       []string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UserInfo     map[string]string
}

// Role 是启用中的角色。Code 为 super_admin 时拥有全部菜单。
type Role struct {
	ID     int64
	Code   string
	Status int
}

// Menu 同时覆盖目录、菜单和按钮。按钮不进菜单树，但 permission 要进权限数组。
type Menu struct {
	ID            int64
	ParentID      int64
	Name          string
	Permission    string
	Type          int
	Sort          int
	Path          string
	Icon          string
	Component     string
	ComponentName string
	Status        int
	Visible       bool
	KeepAlive     bool
	AlwaysShow    bool
}
