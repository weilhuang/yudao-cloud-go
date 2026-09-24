package directory

import "context"

const (
	codeDeptParentNotFound = 1_002_004_001
	codeDeptParentSelf     = 1_002_004_004
	codeDeptParentIsChild  = 1_002_004_007
)

// validateDeptParentChain 与 Java 的 validateParentDept 保持相同的错误码。
// 更新时沿候选父部门向根追溯；已有脏数据形成的环也拒绝继续挂接，避免无界循环。
func validateDeptParentChain(id, parentID int64, parentOf func(int64) (int64, bool, error)) error {
	if parentID == 0 {
		return nil
	}
	if id != 0 && parentID == id {
		return &Error{Code: codeDeptParentSelf, Msg: "不能设置自己为父部门"}
	}
	seen := make(map[int64]bool)
	for current := parentID; current != 0; {
		if id != 0 && current == id {
			return &Error{Code: codeDeptParentIsChild, Msg: "不能设置自己的子部门为父部门"}
		}
		if seen[current] {
			return &Error{Code: codeDeptParentIsChild, Msg: "父部门层级存在环路"}
		}
		seen[current] = true
		next, exists, err := parentOf(current)
		if err != nil {
			return err
		}
		if !exists {
			if current == parentID {
				return &Error{Code: codeDeptParentNotFound, Msg: "父级部门不存在"}
			}
			// Java 遇到历史数据中的缺失祖先时终止追溯。
			return nil
		}
		if id == 0 {
			return nil // 新建部门只需确认直接父部门存在。
		}
		current = next
	}
	return nil
}

// validateDeptParent 读取当前租户的父链；MySQL 写入前还会在事务中重新校验，封闭并发改父的竞态。
func (s *Service) validateDeptParent(ctx context.Context, tenantID, id, parentID int64) error {
	return validateDeptParentChain(id, parentID, func(current int64) (int64, bool, error) {
		if id == 0 {
			ok, err := s.Depts.DeptExists(ctx, tenantID, current)
			return 0, ok, err
		}
		if s.Reader == nil {
			return 0, false, &Error{Code: 500, Msg: "部门读取服务未配置"}
		}
		dept, err := s.Reader.DeptGet(ctx, tenantID, current)
		if err != nil || dept == nil {
			return 0, false, err
		}
		return dept.ParentID, true, nil
	})
}
