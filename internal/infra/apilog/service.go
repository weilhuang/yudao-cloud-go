package apilog

import "context"

// MarkError 把未处理的错误日志改成已处理或已忽略。已经处理过的不能再改。
func MarkError(ctx context.Context, store Store, tenantID, id, userID int64, status int) error {
	current, err := store.ErrorByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_001_002_000, Msg: "API 错误日志不存在"}
	}
	if current.ProcessStatus != 0 {
		return &Error{Code: 1_001_002_001, Msg: "API 错误日志已处理"}
	}
	return store.UpdateErrorStatus(ctx, id, userID, status)
}
