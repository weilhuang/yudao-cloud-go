package directory

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// 系统内置租户使用套餐编号 0，不能改、不能删。
const packageIDSystem = 0

// Tenant 是租户。Websites 是域名列表，ExpireTime 是毫秒时间戳。
type Tenant struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	ContactName   string   `json:"contactName"`
	ContactMobile string   `json:"contactMobile"`
	Status        int      `json:"status"`
	Websites      []string `json:"websites"`
	PackageID     int64    `json:"packageId"`
	ExpireTime    int64    `json:"expireTime"`
	AccountCount  int      `json:"accountCount"`
	Username      string   `json:"username,omitempty"`
	Password      string   `json:"password,omitempty"`
	CreateTime    int64    `json:"createTime,omitempty"`
}

// TenantPackage 是套餐。MenuIDs 是该套餐开通的菜单。
type TenantPackage struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Status     int     `json:"status"`
	Remark     string  `json:"remark"`
	MenuIDs    []int64 `json:"menuIds"`
	CreateTime int64   `json:"createTime,omitempty"`
}

// Notice 是通知公告。
type Notice struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Type       int    `json:"type"`
	Status     int    `json:"status"`
	CreateTime int64  `json:"createTime,omitempty"`
}

// TenantStore 读写租户、套餐、公告和个人资料。
type TenantStore interface {
	TenantByID(ctx context.Context, id int64) (*Tenant, error)
	TenantByName(ctx context.Context, name string) (*Tenant, error)
	TenantByWebsite(ctx context.Context, website string) (*Tenant, error)
	TenantNameTaken(ctx context.Context, name string, exceptID int64) (bool, error)
	TenantWebsiteTaken(ctx context.Context, websites []string, exceptID int64) (string, error)
	TenantPage(ctx context.Context, pageNo, pageSize int, name, contactName string, status *int) (Page[Tenant], error)
	TenantExportRows(ctx context.Context, filter TenantExportFilter, emit func(Tenant) error) error
	TenantSimple(ctx context.Context) ([]Tenant, error)
	CreateTenant(ctx context.Context, tenant Tenant, passwordHash string, menuIDs []int64) (int64, error)
	UpdateTenant(ctx context.Context, tenant Tenant, menuIDs []int64, syncMenus bool) error
	DeleteTenant(ctx context.Context, id int64) error

	PackageByID(ctx context.Context, id int64) (*TenantPackage, error)
	PackageNameTaken(ctx context.Context, name string, exceptID int64) (bool, error)
	PackageUsed(ctx context.Context, id int64) (bool, error)
	PackagePage(ctx context.Context, pageNo, pageSize int, name string, status *int) (Page[TenantPackage], error)
	PackageSimple(ctx context.Context) ([]TenantPackage, error)
	CreatePackage(ctx context.Context, item TenantPackage) (int64, error)
	UpdatePackage(ctx context.Context, item TenantPackage) error
	DeletePackage(ctx context.Context, id int64) error

	NoticePage(ctx context.Context, tenantID int64, pageNo, pageSize int, title string, status *int) (Page[Notice], error)
	NoticeByID(ctx context.Context, tenantID, id int64) (*Notice, error)
	CreateNotice(ctx context.Context, tenantID int64, item Notice) (int64, error)
	UpdateNotice(ctx context.Context, tenantID int64, item Notice) error
	DeleteNotice(ctx context.Context, tenantID, id int64) error

	ProfilePassword(ctx context.Context, userID int64) (string, error)
	UpdateProfile(ctx context.Context, userID int64, nickname, email, mobile, avatar string, sex *int) error
	// UpdateOAuthUser 只写入调用方显式提交的字段。指针为 nil 表示请求里没有该字段，不能写成空串，否则会清掉头像以外的资料。
	UpdateOAuthUser(ctx context.Context, tenantID, userID int64, nickname, email, mobile *string, sex *int) error
	UpdateOwnPassword(ctx context.Context, userID int64, passwordHash string) error
}

// SaveTenant 创建或修改租户。新建时同时生成租户管理员角色和初始账号。
func (s *Service) SaveTenant(ctx context.Context, tenant Tenant) (int64, error) {
	if tenant.Name == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "租户名不能为空"}
	}
	var current *Tenant
	if tenant.ID != 0 {
		var err error
		current, err = s.Tenants.TenantByID(ctx, tenant.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_015_000, Msg: "租户不存在"}
		}
		if current.PackageID == packageIDSystem {
			return 0, &Error{Code: 1_002_015_003, Msg: "系统租户不能进行修改、删除等操作！"}
		}
	} else if tenant.Username == "" || tenant.Password == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "管理员账号和密码不能为空"}
	}
	if tenant.ContactName == "" || tenant.ExpireTime == 0 {
		return 0, &Error{Code: codeBadRequest, Msg: "联系人和过期时间不能为空"}
	}
	if taken, err := s.Tenants.TenantNameTaken(ctx, tenant.Name, tenant.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_015_004, Msg: fmt.Sprintf("名字为【%s】的租户已存在", tenant.Name)}
	}
	if website, err := s.Tenants.TenantWebsiteTaken(ctx, tenant.Websites, tenant.ID); err != nil || website != "" {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_015_005, Msg: fmt.Sprintf("域名为【%s】的租户已存在", website)}
	}
	pkg, err := s.Tenants.PackageByID(ctx, tenant.PackageID)
	if err != nil {
		return 0, err
	}
	if pkg == nil {
		return 0, &Error{Code: 1_002_016_000, Msg: "租户套餐不存在"}
	}
	if pkg.Status != 0 {
		return 0, &Error{Code: 1_002_016_002, Msg: fmt.Sprintf("名字为【%s】的租户套餐已被禁用", pkg.Name)}
	}
	if tenant.ID == 0 {
		hash, err := bcrypt.GenerateFromPassword([]byte(tenant.Password), s.cost())
		if err != nil {
			return 0, err
		}
		return s.Tenants.CreateTenant(ctx, tenant, string(hash), pkg.MenuIDs)
	}
	sync := current.PackageID != tenant.PackageID
	if err := s.Tenants.UpdateTenant(ctx, tenant, pkg.MenuIDs, sync); err != nil {
		return tenant.ID, err
	}
	if sync {
		// 更换套餐会批量改写该租户的角色菜单，提交后同步清理 Java 权限缓存。
		return tenant.ID, s.evictRoleMenusAfterWrite(ctx, tenant.ID)
	}
	return tenant.ID, nil
}

// DeleteTenant 不能删除系统租户。
func (s *Service) DeleteTenant(ctx context.Context, id int64) error {
	current, err := s.Tenants.TenantByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_015_000, Msg: "租户不存在"}
	}
	if current.PackageID == packageIDSystem {
		return &Error{Code: 1_002_015_003, Msg: "系统租户不能进行修改、删除等操作！"}
	}
	return s.Tenants.DeleteTenant(ctx, id)
}

// SavePackage 创建或修改套餐。
func (s *Service) SavePackage(ctx context.Context, item TenantPackage) (int64, error) {
	if item.Name == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "套餐名不能为空"}
	}
	if item.ID != 0 {
		current, err := s.Tenants.PackageByID(ctx, item.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_016_000, Msg: "租户套餐不存在"}
		}
	}
	taken, err := s.Tenants.PackageNameTaken(ctx, item.Name, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_016_003, Msg: "已经存在该名字的租户套餐"}
	}
	if item.ID == 0 {
		return s.Tenants.CreatePackage(ctx, item)
	}
	return item.ID, s.Tenants.UpdatePackage(ctx, item)
}

// DeletePackage 仍被租户使用时不能删。
func (s *Service) DeletePackage(ctx context.Context, id int64) error {
	current, err := s.Tenants.PackageByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_016_000, Msg: "租户套餐不存在"}
	}
	used, err := s.Tenants.PackageUsed(ctx, id)
	if err != nil {
		return err
	}
	if used {
		return &Error{Code: 1_002_016_001, Msg: "租户正在使用该套餐，请给租户重新设置套餐后再尝试删除"}
	}
	return s.Tenants.DeletePackage(ctx, id)
}

// SaveNotice 创建或修改通知公告。
func (s *Service) SaveNotice(ctx context.Context, tenantID int64, item Notice) (int64, error) {
	if item.Title == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "公告标题不能为空"}
	}
	if item.ID != 0 {
		current, err := s.Tenants.NoticeByID(ctx, tenantID, item.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_008_001, Msg: "当前通知公告不存在"}
		}
		return item.ID, s.Tenants.UpdateNotice(ctx, tenantID, item)
	}
	return s.Tenants.CreateNotice(ctx, tenantID, item)
}

// ChangeOwnPassword 校验旧密码后再更新。
func (s *Service) ChangeOwnPassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	if oldPassword == "" || newPassword == "" {
		return &Error{Code: codeBadRequest, Msg: "旧密码和新密码不能为空"}
	}
	hash, err := s.Tenants.ProfilePassword(ctx, userID)
	if err != nil {
		return err
	}
	if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return &Error{Code: 1_002_003_005, Msg: "用户密码校验失败"}
	}
	next, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.cost())
	if err != nil {
		return err
	}
	return s.Tenants.UpdateOwnPassword(ctx, userID, string(next))
}

// ValidTenant 给登录后的请求用：租户要存在、开启、未过期。
func (s *Service) ValidTenant(ctx context.Context, id int64, now time.Time) error {
	tenant, err := s.Tenants.TenantByID(ctx, id)
	if err != nil {
		return err
	}
	if tenant == nil {
		return &Error{Code: 1_002_015_000, Msg: "租户不存在"}
	}
	if tenant.Status != 0 {
		return &Error{Code: 1_002_015_001, Msg: fmt.Sprintf("名字为【%s】的租户已被禁用", tenant.Name)}
	}
	if tenant.ExpireTime > 0 && tenant.ExpireTime < now.UnixMilli() {
		return &Error{Code: 1_002_015_002, Msg: fmt.Sprintf("名字为【%s】的租户已过期", tenant.Name)}
	}
	return nil
}
