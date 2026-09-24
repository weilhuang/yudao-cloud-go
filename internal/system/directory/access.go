package directory

import (
	"context"
	"fmt"
)

const (
	roleTypeSystem      = 1
	roleTypeCustom      = 2
	menuDir             = 1
	menuMenu            = 2
	dataScopeAll        = 1
	dataScopeDeptCustom = 2
	dataScopeSelf       = 5
)

// RoleSave 是角色创建和修改的入参。新建角色类型固定为自定义。
type RoleSave struct {
	ID     int64
	Name   string
	Code   string
	Sort   int
	Status int
	Remark string
	Type   int
}

// MenuSave 是菜单创建和修改的入参。
type MenuSave struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Permission    string `json:"permission"`
	Type          int    `json:"type"`
	Sort          int    `json:"sort"`
	ParentID      int64  `json:"parentId"`
	Path          string `json:"path"`
	Icon          string `json:"icon"`
	Component     string `json:"component"`
	ComponentName string `json:"componentName"`
	Status        int    `json:"status"`
	Visible       bool   `json:"visible"`
	KeepAlive     bool   `json:"keepAlive"`
	AlwaysShow    bool   `json:"alwaysShow"`
	CreateTime    int64  `json:"createTime"`
}

// PostSave 是岗位创建和修改的入参。
type PostSave struct {
	ID     int64
	Name   string
	Code   string
	Sort   int
	Status int
	Remark string
}

// AccessStore 读写角色、菜单、岗位和授权关系。
type AccessStore interface {
	RoleByID(ctx context.Context, tenantID, id int64) (*RoleSave, error)
	RoleNameTaken(ctx context.Context, tenantID int64, name string, exceptID int64) (bool, error)
	RoleCodeTaken(ctx context.Context, tenantID int64, code string, exceptID int64) (bool, error)
	CreateRole(ctx context.Context, tenantID int64, role RoleSave) (int64, error)
	UpdateRole(ctx context.Context, tenantID int64, role RoleSave) error
	UpdateRoleDataScope(ctx context.Context, tenantID, roleID int64, scope int, deptIDs []int64) error
	DeleteRole(ctx context.Context, tenantID, id int64) error
	DeleteRoleList(ctx context.Context, tenantID int64, ids []int64) error

	MenuByID(ctx context.Context, id int64) (*MenuSave, error)
	MenuNameTaken(ctx context.Context, parentID int64, name string, exceptID int64) (bool, error)
	MenuComponentTaken(ctx context.Context, componentName string, exceptID int64) (bool, error)
	MenuChildCount(ctx context.Context, id int64) (int, error)
	CreateMenu(ctx context.Context, menu MenuSave) (int64, error)
	UpdateMenu(ctx context.Context, menu MenuSave) error
	DeleteMenu(ctx context.Context, id int64) error
	DeleteMenuList(ctx context.Context, ids []int64) error

	PostByID(ctx context.Context, tenantID, id int64) (*PostSave, error)
	PostNameTaken(ctx context.Context, tenantID int64, name string, exceptID int64) (bool, error)
	PostCodeTaken(ctx context.Context, tenantID int64, code string, exceptID int64) (bool, error)
	CreatePost(ctx context.Context, tenantID int64, post PostSave) (int64, error)
	UpdatePost(ctx context.Context, tenantID int64, post PostSave) error
	DeletePost(ctx context.Context, tenantID, id int64) error
	DeletePostList(ctx context.Context, tenantID int64, ids []int64) error

	RoleMenuIDs(ctx context.Context, tenantID, roleID int64) ([]int64, error)
	ReplaceRoleMenus(ctx context.Context, tenantID, roleID int64, menuIDs []int64) error
	UserRoleIDs(ctx context.Context, tenantID, userID int64) ([]int64, error)
	ReplaceUserRoles(ctx context.Context, tenantID, userID int64, roleIDs []int64) error
}

// SaveRole 创建或修改角色。系统内置角色不能改，super_admin 这个标识不能新建。
func (s *Service) SaveRole(ctx context.Context, tenantID int64, role RoleSave) (int64, error) {
	if role.Name == "" || role.Code == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "角色名称和标识不能为空"}
	}
	if role.ID == 0 && role.Code == "super_admin" {
		return 0, &Error{Code: 1_002_002_005, Msg: "标识【super_admin】不能使用"}
	}
	if role.ID != 0 {
		current, err := s.Access.RoleByID(ctx, tenantID, role.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_002_000, Msg: "角色不存在"}
		}
		if current.Type == roleTypeSystem {
			return 0, &Error{Code: 1_002_002_003, Msg: "不能操作类型为系统内置的角色"}
		}
	}
	if taken, err := s.Access.RoleNameTaken(ctx, tenantID, role.Name, role.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_002_001, Msg: fmt.Sprintf("已经存在名为【%s】的角色", role.Name)}
	}
	if taken, err := s.Access.RoleCodeTaken(ctx, tenantID, role.Code, role.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_002_002, Msg: fmt.Sprintf("已经存在标识为【%s】的角色", role.Code)}
	}
	if role.ID == 0 {
		role.Type = roleTypeCustom
		return s.Access.CreateRole(ctx, tenantID, role)
	}
	if err := s.Access.UpdateRole(ctx, tenantID, role); err != nil {
		return role.ID, err
	}
	return role.ID, s.evictRoleAfterWrite(ctx, tenantID, role.ID)
}

// DeleteRole 逻辑删除。系统内置角色不能删。
func (s *Service) DeleteRole(ctx context.Context, tenantID, id int64) error {
	current, err := s.Access.RoleByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
	}
	if current.Type == roleTypeSystem {
		return &Error{Code: 1_002_002_003, Msg: "不能操作类型为系统内置的角色"}
	}
	if err := s.Access.DeleteRole(ctx, tenantID, id); err != nil {
		return err
	}
	return s.evictDeletedRoleAfterWrite(ctx, tenantID, id)
}

// DeleteRoleList 在一个事务里校验并删除。任一角色不存在或是内置角色时，整批回滚。
// 缓存按单删同样的范围清理；Java 的批删方法本身没有 CacheEvict，但留下旧角色缓存会造成越权。
func (s *Service) DeleteRoleList(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.Access.DeleteRoleList(ctx, tenantID, ids); err != nil {
		return err
	}
	return s.evictAfterWrite(ctx, "角色批量删除", func(cacheCtx context.Context) error {
		if err := s.JavaRoleCache.EvictDeletedRole(cacheCtx, tenantID, ids[0]); err != nil {
			return err
		}
		for _, id := range ids[1:] {
			if err := s.JavaRoleCache.EvictRole(cacheCtx, tenantID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// AssignRoleDataScope 对齐 Java 的角色类型和范围枚举校验，并收紧指定部门的租户边界。
// 套餐只约束菜单；数据范围使用当前租户自己的部门，不依赖套餐菜单。
func (s *Service) AssignRoleDataScope(ctx context.Context, tenantID, roleID int64, scope int, deptIDs []int64) error {
	if scope < dataScopeAll || scope > dataScopeSelf {
		return &Error{Code: codeBadRequest, Msg: "数据范围不正确"}
	}
	role, err := s.Access.RoleByID(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	if role == nil {
		return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
	}
	if role.Type == roleTypeSystem {
		return &Error{Code: 1_002_002_003, Msg: "不能操作类型为系统内置的角色"}
	}
	if scope != dataScopeDeptCustom {
		// 非自定义范围不用部门编号，避免保留旧范围或调用方误传的部门。
		deptIDs = []int64{}
	} else {
		seen := make(map[int64]struct{}, len(deptIDs))
		unique := make([]int64, 0, len(deptIDs))
		for _, id := range deptIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			if id <= 0 || s.Depts == nil {
				return &Error{Code: 1_002_004_002, Msg: "当前部门不存在"}
			}
			exists, err := s.Depts.DeptExists(ctx, tenantID, id)
			if err != nil {
				return err
			}
			if !exists {
				return &Error{Code: 1_002_004_002, Msg: "当前部门不存在"}
			}
			unique = append(unique, id)
		}
		deptIDs = unique
	}
	if err := s.Access.UpdateRoleDataScope(ctx, tenantID, roleID, scope, deptIDs); err != nil {
		return err
	}
	return s.evictRoleAfterWrite(ctx, tenantID, roleID)
}

// SaveMenu 创建或修改菜单。父级必须是目录或菜单，不能指向自己。
func (s *Service) SaveMenu(ctx context.Context, menu MenuSave) (int64, error) {
	if menu.Name == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "菜单名称不能为空"}
	}
	if menu.ID != 0 && menu.ParentID == menu.ID {
		return 0, &Error{Code: 1_002_001_002, Msg: "不能设置自己为父菜单"}
	}
	if menu.ParentID != 0 {
		parent, err := s.Access.MenuByID(ctx, menu.ParentID)
		if err != nil {
			return 0, err
		}
		if parent == nil {
			return 0, &Error{Code: 1_002_001_001, Msg: "父菜单不存在"}
		}
		if parent.Type != menuDir && parent.Type != menuMenu {
			return 0, &Error{Code: 1_002_001_005, Msg: "父菜单的类型必须是目录或者菜单"}
		}
	}
	if taken, err := s.Access.MenuNameTaken(ctx, menu.ParentID, menu.Name, menu.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_001_000, Msg: "已经存在该名字的菜单"}
	}
	if menu.ComponentName != "" {
		taken, err := s.Access.MenuComponentTaken(ctx, menu.ComponentName, menu.ID)
		if err != nil {
			return 0, err
		}
		if taken {
			return 0, &Error{Code: 1_002_001_006, Msg: "已经存在该组件名的菜单"}
		}
	}
	if menu.ID == 0 {
		id, err := s.Access.CreateMenu(ctx, menu)
		if err != nil {
			return 0, err
		}
		return id, s.evictMenuCreatedAfterWrite(ctx, menu.Permission)
	}
	current, err := s.Access.MenuByID(ctx, menu.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_002_001_003, Msg: "菜单不存在"}
	}
	if err := s.Access.UpdateMenu(ctx, menu); err != nil {
		return menu.ID, err
	}
	return menu.ID, s.evictMenuChangedAfterWrite(ctx)
}

// DeleteMenu 有子菜单时拒绝删除。
func (s *Service) DeleteMenu(ctx context.Context, id int64) error {
	current, err := s.Access.MenuByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_001_003, Msg: "菜单不存在"}
	}
	children, err := s.Access.MenuChildCount(ctx, id)
	if err != nil {
		return err
	}
	if children > 0 {
		return &Error{Code: 1_002_001_004, Msg: "存在子菜单，无法删除"}
	}
	if err := s.Access.DeleteMenu(ctx, id); err != nil {
		return err
	}
	return s.evictMenusDeletedAfterWrite(ctx, []int64{id})
}

// DeleteMenuList 与 Java 批量删除一样，先检查全部子菜单，再在一个事务内
// 标记菜单及角色菜单关系为已删除；不存在的编号不阻止其余菜单删除。
func (s *Service) DeleteMenuList(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return &Error{Code: codeBadRequest, Msg: "菜单编号不能为空"}
	}
	if err := s.Access.DeleteMenuList(ctx, ids); err != nil {
		return err
	}
	return s.evictMenusDeletedAfterWrite(ctx, ids)
}

// SavePost 创建或修改岗位。名称和编码在租户内唯一。
func (s *Service) SavePost(ctx context.Context, tenantID int64, post PostSave) (int64, error) {
	if post.Name == "" || post.Code == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "岗位名称和编码不能为空"}
	}
	if post.ID != 0 {
		current, err := s.Access.PostByID(ctx, tenantID, post.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_005_000, Msg: "当前岗位不存在"}
		}
	}
	if taken, err := s.Access.PostNameTaken(ctx, tenantID, post.Name, post.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_005_002, Msg: "已经存在该名字的岗位"}
	}
	if taken, err := s.Access.PostCodeTaken(ctx, tenantID, post.Code, post.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_005_003, Msg: "已经存在该标识的岗位"}
	}
	if post.ID == 0 {
		return s.Access.CreatePost(ctx, tenantID, post)
	}
	return post.ID, s.Access.UpdatePost(ctx, tenantID, post)
}

// DeletePost 逻辑删除岗位。
func (s *Service) DeletePost(ctx context.Context, tenantID, id int64) error {
	current, err := s.Access.PostByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_005_000, Msg: "当前岗位不存在"}
	}
	return s.Access.DeletePost(ctx, tenantID, id)
}

// DeletePostList 对齐 Java 的 deleteByIds：不存在和重复编号不阻止其他岗位删除。
// 存储层用一条 SQL 更新，整个批次要么一起成功，要么一起失败。
func (s *Service) DeletePostList(ctx context.Context, tenantID int64, ids []int64) error {
	return s.Access.DeletePostList(ctx, tenantID, ids)
}

// AssignRoleMenus 用新的菜单集合替换角色现有菜单。
// Java 管理接口会先过滤套餐外菜单；系统租户以全部未删除菜单为范围。
func (s *Service) AssignRoleMenus(ctx context.Context, tenantID, roleID int64, menuIDs []int64) error {
	current, err := s.Access.RoleByID(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
	}
	tenant, err := s.Tenants.TenantByID(ctx, tenantID)
	if err != nil {
		return err
	}
	if tenant == nil {
		return &Error{Code: 1_002_015_000, Msg: "租户不存在"}
	}
	allowed := make(map[int64]struct{})
	if tenant.PackageID == packageIDSystem {
		// 系统租户不受套餐限制，但仍不能给角色分配不存在或已删除的菜单。
		for _, menuID := range menuIDs {
			menu, err := s.Access.MenuByID(ctx, menuID)
			if err != nil {
				return err
			}
			if menu != nil {
				allowed[menuID] = struct{}{}
			}
		}
	} else {
		pkg, err := s.Tenants.PackageByID(ctx, tenant.PackageID)
		if err != nil {
			return err
		}
		if pkg == nil {
			return &Error{Code: 1_002_016_000, Msg: "租户套餐不存在"}
		}
		for _, menuID := range pkg.MenuIDs {
			allowed[menuID] = struct{}{}
		}
	}
	// 与 Java 的 Set<Long> 和 removeIf 一致：静默移除未开通菜单并去重。
	filtered := make([]int64, 0, len(menuIDs))
	for _, menuID := range menuIDs {
		if _, ok := allowed[menuID]; !ok {
			continue
		}
		filtered = append(filtered, menuID)
		delete(allowed, menuID)
	}
	if err := s.Access.ReplaceRoleMenus(ctx, tenantID, roleID, filtered); err != nil {
		return err
	}
	return s.evictRoleMenusAfterWrite(ctx, tenantID)
}

// AssignUserRoles 用新的角色集合替换用户现有角色。
// 用户与角色都必须属于当前租户，否则可通过提交其他租户的角色编号获得越权权限。
func (s *Service) AssignUserRoles(ctx context.Context, tenantID, userID int64, roleIDs []int64) error {
	user, err := s.Reader.UserGet(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	checked := make(map[int64]struct{}, len(roleIDs))
	uniqueRoleIDs := make([]int64, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		if _, ok := checked[roleID]; ok {
			continue
		}
		checked[roleID] = struct{}{}
		uniqueRoleIDs = append(uniqueRoleIDs, roleID)
		role, err := s.Access.RoleByID(ctx, tenantID, roleID)
		if err != nil {
			return err
		}
		if role == nil {
			return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
		}
		if role.Status != 0 {
			return &Error{Code: 1_002_002_004, Msg: fmt.Sprintf("名字为【%s】的角色已被禁用", role.Name)}
		}
	}
	if err := s.Access.ReplaceUserRoles(ctx, tenantID, userID, uniqueRoleIDs); err != nil {
		return err
	}
	return s.evictUserRolesAfterWrite(ctx, userID)
}
