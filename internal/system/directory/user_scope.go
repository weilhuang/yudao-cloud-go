package directory

import (
	"context"
)

type userAccessKey struct{}

func withUserAccess(ctx context.Context, access UserAccess) context.Context {
	return context.WithValue(ctx, userAccessKey{}, access)
}

func accessFrom(ctx context.Context) *UserAccess {
	access, ok := ctx.Value(userAccessKey{}).(UserAccess)
	if !ok {
		return nil
	}
	copied := access
	return &copied
}

// expandUserQuery 把请求里的部门展开成自身加后代，并把上下文中的数据范围带进 SQL。
func (m *MySQL) expandUserQuery(ctx context.Context, tenantID int64, query UserQuery) (UserQuery, error) {
	if query.Access == nil {
		query.Access = accessFrom(ctx)
	}
	if query.DeptID == nil {
		return query, nil
	}
	children, err := m.childDeptIDs(ctx, tenantID, *query.DeptID)
	if err != nil {
		return query, err
	}
	query.DeptIDs = append([]int64{*query.DeptID}, children...)
	query.DeptID = nil
	return query, nil
}

// childDeptIDs 只返回后代，不包含起点。环路用已访问集合截断。
func (m *MySQL) childDeptIDs(ctx context.Context, tenantID, deptID int64) ([]int64, error) {
	parents := []int64{deptID}
	seen := map[int64]struct{}{deptID: {}}
	result := make([]int64, 0)
	for depth := 0; depth < 32767 && len(parents) > 0; depth++ {
		marks := ""
		args := make([]any, 0, len(parents)+1)
		args = append(args, tenantID)
		for i, id := range parents {
			if i > 0 {
				marks += ","
			}
			marks += "?"
			args = append(args, id)
		}
		rows, err := m.DB.QueryContext(ctx, `SELECT id FROM system_dept
			WHERE tenant_id=? AND deleted=0 AND parent_id IN (`+marks+`) ORDER BY id`, args...)
		if err != nil {
			return nil, err
		}
		next := make([]int64, 0)
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
			next = append(next, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		parents = next
	}
	return result, nil
}
