package directory

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// DeleteUser 删除单个用户。用户不在本租户时返回业务错误，不清理其它租户的数据。
func (m *MySQL) DeleteUser(ctx context.Context, tenantID, id int64) error {
	found, err := m.deleteUsers(ctx, tenantID, []int64{id})
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	return nil
}

// DeleteUserList 批量删除。缺失编号略过，返回实际删除的编号。
func (m *MySQL) DeleteUserList(ctx context.Context, tenantID int64, ids []int64) ([]int64, error) {
	return m.deleteUsers(ctx, tenantID, ids)
}

// deleteUsers 在一个事务里逻辑删除用户，以及该用户在本租户的角色和岗位关系。
// 条件始终带 tenant_id，避免历史脏数据把别的租户关系一起改掉。
func (m *MySQL) deleteUsers(ctx context.Context, tenantID int64, input []int64) ([]int64, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if len(input) > 1000 {
		return nil, &Error{Code: codeBadRequest, Msg: "请求参数过多"}
	}
	seen := make(map[int64]struct{}, len(input))
	ids := make([]int64, 0, len(input))
	for _, id := range input {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, tenantID)
	for _, id := range ids {
		args = append(args, id)
	}

	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM system_users
		WHERE tenant_id=? AND deleted=0 AND id IN (`+marks+`) ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return nil, err
	}
	found := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(found) == 0 {
		return nil, tx.Commit()
	}

	foundArgs := make([]any, 0, len(found)+1)
	foundArgs = append(foundArgs, tenantID)
	for _, id := range found {
		foundArgs = append(foundArgs, id)
	}
	foundMarks := strings.TrimSuffix(strings.Repeat("?,", len(found)), ",")
	if _, err := tx.ExecContext(ctx, `UPDATE system_users SET deleted=1
		WHERE tenant_id=? AND deleted=0 AND id IN (`+foundMarks+`)`, foundArgs...); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_user_role SET deleted=1
		WHERE tenant_id=? AND deleted=0 AND user_id IN (`+foundMarks+`)`, foundArgs...); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE system_user_post SET deleted=1
		WHERE tenant_id=? AND deleted=0 AND user_id IN (`+foundMarks+`)`, foundArgs...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return found, nil
}
