package rolepostrpc

import (
	"context"
	"database/sql"
	"strings"
)

type MySQL struct{ DB *sql.DB }

// Posts 显式约束租户和逻辑删除；不按 status 过滤，供 list 与 valid 共用。
func (m *MySQL) Posts(ctx context.Context, tenantID int64, ids []int64) ([]Post, error) {
	if len(ids) == 0 {
		return []Post{}, nil
	}
	args := idArgs(tenantID, ids)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code, sort, status FROM system_post
		WHERE tenant_id = ? AND deleted = 0 AND id IN (`+placeholders(len(ids))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	posts := make([]Post, 0, len(ids))
	for rows.Next() {
		var post Post
		if err := rows.Scan(&post.ID, &post.Name, &post.Code, &post.Sort, &post.Status); err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, rows.Err()
}

// Roles 的查询规则与 Java roleMapper.selectByIds 一致：包含停用，排除逻辑删除及其他租户。
func (m *MySQL) Roles(ctx context.Context, tenantID int64, ids []int64) ([]Role, error) {
	if len(ids) == 0 {
		return []Role{}, nil
	}
	args := idArgs(tenantID, ids)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code, sort, status FROM system_role
		WHERE tenant_id = ? AND deleted = 0 AND id IN (`+placeholders(len(ids))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]Role, 0, len(ids))
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Code, &role.Sort, &role.Status); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func idArgs(tenantID int64, ids []int64) []any {
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
