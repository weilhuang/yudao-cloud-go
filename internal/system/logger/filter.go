package logger

import "fmt"

// loginFilter 拼登录日志的 WHERE。status=true 只看成功，false 看 result>0 的失败。
func loginFilter(tenantID int64, q LoginQuery) (string, []any) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if q.UserIP != "" {
		where += ` AND user_ip LIKE ?`
		args = append(args, "%"+q.UserIP+"%")
	}
	if q.Username != "" {
		where += ` AND username LIKE ?`
		args = append(args, "%"+q.Username+"%")
	}
	if q.Success != nil {
		if *q.Success {
			where += ` AND result=0`
		} else {
			where += ` AND result>0`
		}
	}
	where, args = appendTime(where, args, "create_time", q.Start, q.End)
	return where, args
}

func operateFilter(tenantID int64, q OperateQuery) (string, []any) {
	where := `WHERE o.deleted=0 AND o.tenant_id=?`
	args := []any{tenantID}
	if q.UserID != 0 {
		where += ` AND o.user_id=?`
		args = append(args, q.UserID)
	}
	if q.BizID != 0 {
		where += ` AND o.biz_id=?`
		args = append(args, q.BizID)
	}
	if q.Type != "" {
		where += ` AND o.type LIKE ?`
		args = append(args, "%"+q.Type+"%")
	}
	if q.SubType != "" {
		where += ` AND o.sub_type LIKE ?`
		args = append(args, "%"+q.SubType+"%")
	}
	if q.Action != "" {
		where += ` AND o.action LIKE ?`
		args = append(args, "%"+q.Action+"%")
	}
	where, args = appendTime(where, args, "o.create_time", q.Start, q.End)
	return where, args
}

func appendTime(where string, args []any, column string, start, end *int64) (string, []any) {
	if start != nil {
		where += fmt.Sprintf(` AND %s>=FROM_UNIXTIME(?/1000)`, column)
		args = append(args, *start)
	}
	if end != nil {
		where += fmt.Sprintf(` AND %s<=FROM_UNIXTIME(?/1000)`, column)
		args = append(args, *end)
	}
	return where, args
}

func normalizePage(pageNo, pageSize int) (int, int, bool) {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize < 0 {
		return pageNo, 10000, true
	}
	if pageSize == 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return pageNo, pageSize, false
}
