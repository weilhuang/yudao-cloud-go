package permissionrpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// MySQL 只提供权限判断所需的读查询；管理接口的写操作仍由 directory 负责。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) UserIDsByRoleIDs(ctx context.Context, tenantID int64, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return []int64{}, nil
	}
	args := append([]any{tenantID}, intArgs(roleIDs)...)
	rows, err := m.DB.QueryContext(ctx, `SELECT DISTINCT ur.user_id FROM system_user_role ur
		WHERE ur.tenant_id=? AND ur.deleted=0 AND ur.role_id IN (`+places(len(roleIDs))+`) ORDER BY ur.user_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (m *MySQL) EnabledRolesByUser(ctx context.Context, tenantID, userID int64) ([]Role, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT r.id, r.code, r.data_scope, IFNULL(r.data_scope_dept_ids,'')
		FROM system_user_role ur JOIN system_role r ON r.id=ur.role_id AND r.tenant_id=ur.tenant_id
		WHERE ur.tenant_id=? AND ur.user_id=? AND ur.deleted=0 AND r.deleted=0 AND r.status=0
		ORDER BY r.id`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]Role, 0)
	seen := map[int64]struct{}{}
	for rows.Next() {
		var role Role
		var scope sql.NullInt64
		var scopeDeptIDs string
		if err := rows.Scan(&role.ID, &role.Code, &scope, &scopeDeptIDs); err != nil {
			return nil, err
		}
		if _, ok := seen[role.ID]; ok {
			continue
		}
		seen[role.ID] = struct{}{}
		if scope.Valid {
			role.DataScope = int(scope.Int64)
		}
		if scopeDeptIDs != "" {
			if err := json.Unmarshal([]byte(scopeDeptIDs), &role.ScopeDeptIDs); err != nil {
				return nil, err
			}
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// RolesHavePermission 与 Java 先查精确 permission 菜单、再查菜单所属角色等价。
// 菜单状态在 Java 的这条判断路径中不参与过滤。
func (m *MySQL) RolesHavePermission(ctx context.Context, tenantID int64, roleIDs []int64, permission string) (bool, error) {
	if len(roleIDs) == 0 {
		return false, nil
	}
	args := append([]any{permission, tenantID}, intArgs(roleIDs)...)
	var exists int
	err := m.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM system_menu menu JOIN system_role_menu rm ON rm.menu_id=menu.id
		WHERE menu.permission=? AND menu.deleted=0 AND rm.tenant_id=? AND rm.deleted=0
		AND rm.role_id IN (`+places(len(roleIDs))+`))`, args...).Scan(&exists)
	return exists != 0, err
}

func (m *MySQL) UserDeptID(ctx context.Context, tenantID, userID int64) (*int64, error) {
	var dept sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT dept_id FROM system_users
		WHERE tenant_id=? AND id=? AND deleted=0`, tenantID, userID).Scan(&dept)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || !dept.Valid {
		return nil, err
	}
	return &dept.Int64, nil
}

// ChildDeptIDs 按层查询部门后代；脏数据中的环路用已访问集合截断。
func (m *MySQL) ChildDeptIDs(ctx context.Context, tenantID, deptID int64) ([]int64, error) {
	parents := []int64{deptID}
	seen := map[int64]struct{}{deptID: {}}
	result := make([]int64, 0)
	for depth := 0; depth < 32767 && len(parents) > 0; depth++ {
		args := append([]any{tenantID}, intArgs(parents)...)
		rows, err := m.DB.QueryContext(ctx, `SELECT id FROM system_dept
			WHERE tenant_id=? AND deleted=0 AND parent_id IN (`+places(len(parents))+`) ORDER BY id`, args...)
		if err != nil {
			return nil, err
		}
		ids, err := scanIDs(rows)
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		parents = parents[:0]
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
			parents = append(parents, id)
		}
	}
	return result, nil
}

func places(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func intArgs(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

func scanIDs(rows *sql.Rows) ([]int64, error) {
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
