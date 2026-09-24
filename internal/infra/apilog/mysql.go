package apilog

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MySQL 读写 infra_api_access_log 和 infra_api_error_log。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) CreateAccess(ctx context.Context, tenantID int64, item AccessLog) error {
	begin := millisOrNow(item.BeginTime)
	end := millisOrNow(item.EndTime)
	_, err := m.DB.ExecContext(ctx, `INSERT INTO infra_api_access_log
		(trace_id, user_id, user_type, application_name, request_method, request_url, request_params, response_body,
		 user_ip, user_agent, operate_module, operate_name, operate_type, begin_time, end_time, duration, result_code, result_msg,
		 tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, FROM_UNIXTIME(?/1000), FROM_UNIXTIME(?/1000), ?, ?, ?, ?, 0, ?)`,
		cut(item.TraceID, 64), item.UserID, item.UserType, cut(item.ApplicationName, 50), cut(item.RequestMethod, 16), cut(item.RequestURL, 255),
		item.RequestParams, item.ResponseBody, cut(empty(item.UserIP), 50), cut(empty(item.UserAgent), 512), cut(item.OperateModule, 50), cut(item.OperateName, 50),
		item.OperateType, begin, end, item.Duration, item.ResultCode, cut(item.ResultMsg, 512), tenantID, time.Now())
	return err
}

func (m *MySQL) AccessPage(ctx context.Context, tenantID int64, q AccessQuery) (Page[AccessLog], error) {
	where, args := accessFilter(tenantID, q)
	return queryPage(ctx, m.DB, `SELECT COUNT(*) FROM infra_api_access_log `+where,
		`SELECT id, IFNULL(trace_id,''), user_id, user_type, application_name, request_method, request_url, IFNULL(request_params,''), IFNULL(response_body,''),
			IFNULL(user_ip,''), IFNULL(user_agent,''), IFNULL(operate_module,''), IFNULL(operate_name,''), IFNULL(operate_type,0),
			IFNULL(UNIX_TIMESTAMP(begin_time),0)*1000, IFNULL(UNIX_TIMESTAMP(end_time),0)*1000, duration, result_code, IFNULL(result_msg,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
			FROM infra_api_access_log `+where+` ORDER BY id DESC`, args, q.PageNo, q.PageSize, scanAccess)
}

func (m *MySQL) AccessByID(ctx context.Context, tenantID, id int64) (*AccessLog, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT id, IFNULL(trace_id,''), user_id, user_type, application_name, request_method, request_url, IFNULL(request_params,''), IFNULL(response_body,''),
		IFNULL(user_ip,''), IFNULL(user_agent,''), IFNULL(operate_module,''), IFNULL(operate_name,''), IFNULL(operate_type,0),
		IFNULL(UNIX_TIMESTAMP(begin_time),0)*1000, IFNULL(UNIX_TIMESTAMP(end_time),0)*1000, duration, result_code, IFNULL(result_msg,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
		FROM infra_api_access_log WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	item, err := scanAccess(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

func (m *MySQL) CreateError(ctx context.Context, tenantID int64, item ErrorLog) error {
	when := millisOrNow(item.ExceptionTime)
	_, err := m.DB.ExecContext(ctx, `INSERT INTO infra_api_error_log
		(trace_id, user_id, user_type, application_name, request_method, request_url, request_params, user_ip, user_agent,
		 exception_time, exception_name, exception_message, exception_root_cause_message, exception_stack_trace,
		 exception_class_name, exception_file_name, exception_method_name, exception_line_number, process_status, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, FROM_UNIXTIME(?/1000), ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 0, ?)`,
		cut(item.TraceID, 64), item.UserID, item.UserType, cut(item.ApplicationName, 50), cut(item.RequestMethod, 16), cut(item.RequestURL, 255),
		cut(item.RequestParams, 8000), cut(empty(item.UserIP), 50), cut(empty(item.UserAgent), 512), when,
		cut(item.ExceptionName, 128), item.ExceptionMessage, item.ExceptionRootCauseMessage, item.ExceptionStackTrace,
		cut(item.ExceptionClassName, 512), cut(item.ExceptionFileName, 512), cut(item.ExceptionMethodName, 512), item.ExceptionLineNumber,
		tenantID, time.Now())
	return err
}

func (m *MySQL) ErrorPage(ctx context.Context, tenantID int64, q ErrorQuery) (Page[ErrorLog], error) {
	where, args := errorFilter(tenantID, q)
	return queryPage(ctx, m.DB, `SELECT COUNT(*) FROM infra_api_error_log `+where, errorSelect+` `+where+` ORDER BY id DESC`, args, q.PageNo, q.PageSize, scanError)
}

func (m *MySQL) ErrorByID(ctx context.Context, tenantID, id int64) (*ErrorLog, error) {
	row := m.DB.QueryRowContext(ctx, errorSelect+` WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	item, err := scanError(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

func (m *MySQL) UpdateErrorStatus(ctx context.Context, id, userID int64, status int) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_api_error_log SET process_status=?, process_user_id=?, process_time=? WHERE id=? AND deleted=0`,
		status, userID, time.Now(), id)
	return err
}

const errorSelect = `SELECT id, IFNULL(trace_id,''), user_id, user_type, application_name, request_method, request_url, IFNULL(request_params,''),
	IFNULL(user_ip,''), IFNULL(user_agent,''), IFNULL(UNIX_TIMESTAMP(exception_time),0)*1000, IFNULL(exception_name,''), IFNULL(exception_message,''),
	IFNULL(exception_root_cause_message,''), IFNULL(exception_stack_trace,''), IFNULL(exception_class_name,''), IFNULL(exception_file_name,''),
	IFNULL(exception_method_name,''), exception_line_number, process_status, UNIX_TIMESTAMP(process_time)*1000, IFNULL(process_user_id,0), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
	FROM infra_api_error_log`

func accessFilter(tenantID int64, q AccessQuery) (string, []any) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if q.UserID != 0 {
		where += ` AND user_id=?`
		args = append(args, q.UserID)
	}
	if q.UserType != nil {
		where += ` AND user_type=?`
		args = append(args, *q.UserType)
	}
	if q.ApplicationName != "" {
		where += ` AND application_name=?`
		args = append(args, q.ApplicationName)
	}
	if q.RequestURL != "" {
		where += ` AND request_url LIKE ?`
		args = append(args, "%"+q.RequestURL+"%")
	}
	where, args = between(where, args, "begin_time", q.Start, q.End)
	if q.Duration != nil {
		where += ` AND duration>=?`
		args = append(args, *q.Duration)
	}
	if q.ResultCode != nil {
		where += ` AND result_code=?`
		args = append(args, *q.ResultCode)
	}
	return where, args
}

func errorFilter(tenantID int64, q ErrorQuery) (string, []any) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if q.UserID != 0 {
		where += ` AND user_id=?`
		args = append(args, q.UserID)
	}
	if q.UserType != nil {
		where += ` AND user_type=?`
		args = append(args, *q.UserType)
	}
	if q.ApplicationName != "" {
		where += ` AND application_name=?`
		args = append(args, q.ApplicationName)
	}
	if q.RequestURL != "" {
		where += ` AND request_url LIKE ?`
		args = append(args, "%"+q.RequestURL+"%")
	}
	where, args = between(where, args, "exception_time", q.Start, q.End)
	if q.ProcessStatus != nil {
		where += ` AND process_status=?`
		args = append(args, *q.ProcessStatus)
	}
	return where, args
}

func between(where string, args []any, column string, start, end *int64) (string, []any) {
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

type scanner interface {
	Scan(dest ...any) error
}

func scanAccess(row scanner) (*AccessLog, error) {
	var item AccessLog
	err := row.Scan(&item.ID, &item.TraceID, &item.UserID, &item.UserType, &item.ApplicationName, &item.RequestMethod, &item.RequestURL,
		&item.RequestParams, &item.ResponseBody, &item.UserIP, &item.UserAgent, &item.OperateModule, &item.OperateName, &item.OperateType,
		&item.BeginTime, &item.EndTime, &item.Duration, &item.ResultCode, &item.ResultMsg, &item.CreateTime)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanError(row scanner) (*ErrorLog, error) {
	var item ErrorLog
	var process sql.NullInt64
	err := row.Scan(&item.ID, &item.TraceID, &item.UserID, &item.UserType, &item.ApplicationName, &item.RequestMethod, &item.RequestURL,
		&item.RequestParams, &item.UserIP, &item.UserAgent, &item.ExceptionTime, &item.ExceptionName, &item.ExceptionMessage,
		&item.ExceptionRootCauseMessage, &item.ExceptionStackTrace, &item.ExceptionClassName, &item.ExceptionFileName,
		&item.ExceptionMethodName, &item.ExceptionLineNumber, &item.ProcessStatus, &process, &item.ProcessUserID, &item.CreateTime)
	if err != nil {
		return nil, err
	}
	if process.Valid {
		item.ProcessTime = &process.Int64
	}
	return &item, nil
}

func queryPage[T any](ctx context.Context, db *sql.DB, countSQL, listSQL string, args []any, pageNo, pageSize int, scan func(scanner) (*T, error)) (Page[T], error) {
	var total int64
	if err := db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return Page[T]{}, err
	}
	pageNo, pageSize, all := normalizePage(pageNo, pageSize)
	listArgs := append([]any{}, args...)
	query := listSQL
	if !all {
		query += ` LIMIT ? OFFSET ?`
		listArgs = append(listArgs, pageSize, (pageNo-1)*pageSize)
	} else {
		query += ` LIMIT ?`
		listArgs = append(listArgs, pageSize)
	}
	rows, err := db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return Page[T]{}, err
	}
	defer rows.Close()
	list := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return Page[T]{}, err
		}
		list = append(list, *item)
	}
	return Page[T]{List: list, Total: total}, rows.Err()
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

func millisOrNow(ms int64) int64 {
	if ms == 0 {
		return time.Now().UnixMilli()
	}
	return ms
}

func empty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cut(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
