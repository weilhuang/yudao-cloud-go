package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// RPCUsers 只投影 Java AdminUserRespDTO 的字段；查询、逻辑删除和岗位关系都按租户约束。
func (m *MySQL) RPCUsers(ctx context.Context, tenantID int64, filter RPCUserFilter) ([]RPCUser, error) {
	query := `SELECT u.id, u.nickname, u.status, u.dept_id, u.post_ids,
		IFNULL(u.mobile,''), IFNULL(u.email,''), u.sex, IFNULL(u.avatar,'')
		FROM system_users u WHERE u.tenant_id = ? AND u.deleted = 0`
	args := []any{tenantID}
	switch {
	case filter.IDs != nil:
		if len(filter.IDs) == 0 {
			return []RPCUser{}, nil
		}
		query += ` AND u.id IN (` + rpcPlaces(len(filter.IDs)) + `)`
		args = rpcArgs(args, filter.IDs)
	case filter.DeptIDs != nil:
		if len(filter.DeptIDs) == 0 {
			return []RPCUser{}, nil
		}
		query += ` AND u.dept_id IN (` + rpcPlaces(len(filter.DeptIDs)) + `)`
		args = rpcArgs(args, filter.DeptIDs)
	case filter.PostIDs != nil:
		if len(filter.PostIDs) == 0 {
			return []RPCUser{}, nil
		}
		query += ` AND EXISTS (SELECT 1 FROM system_user_post up WHERE up.user_id = u.id
			AND up.tenant_id = u.tenant_id AND up.deleted = 0 AND up.post_id IN (` + rpcPlaces(len(filter.PostIDs)) + `))`
		args = rpcArgs(args, filter.PostIDs)
	case filter.Mobile != nil:
		query += ` AND u.mobile = ?`
		args = append(args, *filter.Mobile)
	case filter.Nickname != nil:
		query += ` AND u.nickname LIKE ?`
		args = append(args, "%"+*filter.Nickname+"%")
	default:
		return []RPCUser{}, nil
	}
	if filter.ApplyAccess {
		clause, scopeArgs := accessSQL(accessFrom(ctx))
		query += clause
		args = append(args, scopeArgs...)
	}
	rows, err := m.DB.QueryContext(ctx, query+` ORDER BY u.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]RPCUser, 0)
	for rows.Next() {
		var user RPCUser
		var dept sql.NullInt64
		var posts sql.NullString
		var sex sql.NullInt64
		if err := rows.Scan(&user.ID, &user.Nickname, &user.Status, &dept, &posts,
			&user.Mobile, &user.Email, &sex, &user.Avatar); err != nil {
			return nil, err
		}
		if dept.Valid {
			user.DeptID = &dept.Int64
		}
		if sex.Valid {
			value := int(sex.Int64)
			user.Sex = &value
		}
		if posts.Valid {
			if err := json.Unmarshal([]byte(posts.String), &user.PostIDs); err != nil {
				return nil, err
			}
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (m *MySQL) RPCDepts(ctx context.Context, tenantID int64, ids []int64) ([]RPCDept, error) {
	if len(ids) == 0 {
		return []RPCDept{}, nil
	}
	query := `SELECT id, name, parent_id, leader_user_id, status FROM system_dept
		WHERE tenant_id = ? AND deleted = 0 AND id IN (` + rpcPlaces(len(ids)) + `) ORDER BY id`
	return m.rpcDepts(ctx, query, rpcArgs([]any{tenantID}, ids))
}

func (m *MySQL) RPCChildDepts(ctx context.Context, tenantID int64, parentIDs []int64) ([]RPCDept, error) {
	if len(parentIDs) == 0 {
		return []RPCDept{}, nil
	}
	query := `SELECT id, name, parent_id, leader_user_id, status FROM system_dept
		WHERE tenant_id = ? AND deleted = 0 AND parent_id IN (` + rpcPlaces(len(parentIDs)) + `) ORDER BY id`
	return m.rpcDepts(ctx, query, rpcArgs([]any{tenantID}, parentIDs))
}

func (m *MySQL) rpcDepts(ctx context.Context, query string, args []any) ([]RPCDept, error) {
	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]RPCDept, 0)
	for rows.Next() {
		var dept RPCDept
		var leader sql.NullInt64
		if err := rows.Scan(&dept.ID, &dept.Name, &dept.ParentID, &leader, &dept.Status); err != nil {
			return nil, err
		}
		if leader.Valid {
			dept.LeaderUserID = &leader.Int64
		}
		list = append(list, dept)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return filterRPCDepts(ctx, list), nil
}

func filterRPCDepts(ctx context.Context, list []RPCDept) []RPCDept {
	access := accessFrom(ctx)
	if access == nil || access.All {
		return list
	}
	out := make([]RPCDept, 0, len(list))
	for _, dept := range list {
		if deptVisible(access, dept.ID) {
			out = append(out, dept)
		}
	}
	return out
}

func rpcPlaces(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func rpcArgs(dst []any, ids []int64) []any {
	for _, id := range ids {
		dst = append(dst, id)
	}
	return dst
}
