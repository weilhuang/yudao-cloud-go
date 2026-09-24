package directory

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

const (
	codeDeptNotFound = 1_002_004_002
	codeDeptHasChild = 1_002_004_003
)

// DeleteDept 对齐 Java 单删：先确认本租户部门存在，再拒绝仍有子部门的删除。
func (m *MySQL) DeleteDept(ctx context.Context, tenantID, id int64) error {
	return m.deleteDeptIDs(ctx, tenantID, []int64{id}, true)
}

// DeleteDeptList 对齐 Java 批删：缺失 ID 略过，但任一 ID 有子部门则整批不删。
func (m *MySQL) DeleteDeptList(ctx context.Context, tenantID int64, ids []int64) error {
	return m.deleteDeptIDs(ctx, tenantID, ids, false)
}

func (m *MySQL) deleteDeptIDs(ctx context.Context, tenantID int64, input []int64, requireExisting bool) error {
	if len(input) == 0 {
		return nil
	}
	if len(input) > 1000 {
		return &Error{Code: codeBadRequest, Msg: "请求参数过多"}
	}
	// 顺序锁定目标行，减少并发批删的锁顺序差异；去重也避免重复占位符。
	seen := make(map[int64]struct{}, len(input))
	ids := make([]int64, 0, len(input))
	for _, id := range input {
		if _, exists := seen[id]; exists {
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
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM system_dept WHERE tenant_id=? AND deleted=0 AND id IN (`+marks+`) ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		count++
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if requireExisting && count == 0 {
		return &Error{Code: codeDeptNotFound, Msg: "当前部门不存在"}
	}

	// 与 Java selectCountByParentId 一样，对传入的每个 ID 检查直接子部门。
	// 检查覆盖缺失 ID，避免脏数据中的孤儿子部门被静默跳过。
	var childID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM system_dept WHERE tenant_id=? AND deleted=0 AND parent_id IN (`+marks+`) LIMIT 1`, args...).Scan(&childID)
	if err == nil {
		return &Error{Code: codeDeptHasChild, Msg: "存在子部门，无法删除"}
	}
	if err != sql.ErrNoRows {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE system_dept SET deleted=1 WHERE tenant_id=? AND deleted=0 AND id IN (`+marks+`)`, args...); err != nil {
		return err
	}
	return tx.Commit()
}
