package permissionrpc

import (
	"context"
	"sort"
)

const (
	scopeAll          = 1
	scopeDeptCustom   = 2
	scopeDeptOnly     = 3
	scopeDeptAndChild = 4
	scopeSelf         = 5
	superAdminCode    = "super_admin"
)

// Role 是权限计算需要的已启用角色。Reader 负责排除已删除或停用的角色。
type Role struct {
	ID           int64
	Code         string
	DataScope    int
	ScopeDeptIDs []int64
}

// Reader 把权限规则和持久化分开；每个方法必须按 tenantID 限定数据。
type Reader interface {
	UserIDsByRoleIDs(context.Context, int64, []int64) ([]int64, error)
	EnabledRolesByUser(context.Context, int64, int64) ([]Role, error)
	RolesHavePermission(context.Context, int64, []int64, string) (bool, error)
	UserDeptID(context.Context, int64, int64) (*int64, error)
	ChildDeptIDs(context.Context, int64, int64) ([]int64, error)
}

type Service struct {
	Reader Reader
}

// DeptDataPermission 与 Java 的 DeptDataPermissionRespDTO 字段一致。
type DeptDataPermission struct {
	All     bool    `json:"all"`
	Self    bool    `json:"self"`
	DeptIDs []int64 `json:"deptIds"`
}

// UserIDsByRoleIDs 对应 Java UserRoleMapper.selectListByRoleIds；不要求角色或用户处于启用状态。
func (s *Service) UserIDsByRoleIDs(ctx context.Context, tenantID int64, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return []int64{}, nil
	}
	ids, err := s.Reader.UserIDsByRoleIDs(ctx, tenantID, roleIDs)
	if err != nil {
		return nil, err
	}
	return uniqueSorted(ids), nil
}

// HasAnyPermissions 严格匹配权限标识。只有启用中的 super_admin 才能在普通匹配失败后兜底。
func (s *Service) HasAnyPermissions(ctx context.Context, tenantID, userID int64, permissions []string) (bool, error) {
	if len(permissions) == 0 {
		return true, nil
	}
	roles, err := s.Reader.EnabledRolesByUser(ctx, tenantID, userID)
	if err != nil || len(roles) == 0 {
		return false, err
	}
	roleIDs := make([]int64, 0, len(roles))
	superAdmin := false
	for _, role := range roles {
		roleIDs = append(roleIDs, role.ID)
		superAdmin = superAdmin || role.Code == superAdminCode
	}
	for _, permission := range permissions {
		matched, err := s.Reader.RolesHavePermission(ctx, tenantID, roleIDs, permission)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return superAdmin, nil
}

// HasAnyRoles 用角色标识而非编号判断，停用角色没有权限。
func (s *Service) HasAnyRoles(ctx context.Context, tenantID, userID int64, codes []string) (bool, error) {
	if len(codes) == 0 {
		return true, nil
	}
	roles, err := s.Reader.EnabledRolesByUser(ctx, tenantID, userID)
	if err != nil {
		return false, err
	}
	for _, wanted := range codes {
		for _, role := range roles {
			if role.Code == wanted {
				return true, nil
			}
		}
	}
	return false, nil
}

// GetDeptDataPermission 合并多个角色的数据范围；查本人部门只在需要时发生。
func (s *Service) GetDeptDataPermission(ctx context.Context, tenantID, userID int64) (DeptDataPermission, error) {
	result := DeptDataPermission{DeptIDs: []int64{}}
	roles, err := s.Reader.EnabledRolesByUser(ctx, tenantID, userID)
	if err != nil {
		return result, err
	}
	if len(roles) == 0 {
		result.Self = true
		return result, nil
	}
	deptSet := map[int64]struct{}{}
	var ownDept *int64
	loadedOwnDept := false
	loadOwnDept := func() (*int64, error) {
		if !loadedOwnDept {
			ownDept, err = s.Reader.UserDeptID(ctx, tenantID, userID)
			loadedOwnDept = true
		}
		return ownDept, err
	}
	for _, role := range roles {
		switch role.DataScope {
		case scopeAll:
			result.All = true
		case scopeDeptCustom:
			for _, id := range role.ScopeDeptIDs {
				deptSet[id] = struct{}{}
			}
			dept, err := loadOwnDept()
			if err != nil {
				return result, err
			}
			if dept != nil {
				deptSet[*dept] = struct{}{}
			}
		case scopeDeptOnly:
			dept, err := loadOwnDept()
			if err != nil {
				return result, err
			}
			if dept != nil {
				deptSet[*dept] = struct{}{}
			}
		case scopeDeptAndChild:
			dept, err := loadOwnDept()
			if err != nil {
				return result, err
			}
			if dept == nil {
				continue
			}
			children, err := s.Reader.ChildDeptIDs(ctx, tenantID, *dept)
			if err != nil {
				return result, err
			}
			for _, id := range children {
				deptSet[id] = struct{}{}
			}
			deptSet[*dept] = struct{}{}
		case scopeSelf:
			result.Self = true
		}
	}
	for id := range deptSet {
		result.DeptIDs = append(result.DeptIDs, id)
	}
	sort.Slice(result.DeptIDs, func(i, j int) bool { return result.DeptIDs[i] < result.DeptIDs[j] })
	return result, nil
}

func uniqueSorted(ids []int64) []int64 {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	result := make([]int64, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
