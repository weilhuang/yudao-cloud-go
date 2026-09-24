package logger

import (
	"context"
	"database/sql"
	"time"

	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// MySQL 读写登录日志和操作日志。两张表都在共享库里，system 进程可以直接写。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) RecordLogin(ctx context.Context, item auth.LoginRecord) error {
	_, err := m.DB.ExecContext(ctx, `INSERT INTO system_login_log
		(log_type, trace_id, user_id, user_type, username, result, user_ip, user_agent, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.LogType, cut(item.TraceID, 64), item.UserID, item.UserType, cut(item.Username, 50), item.Result,
		cut(item.UserIP, 50), cut(item.UserAgent, 512), item.TenantID, time.Now())
	return err
}

func (m *MySQL) LoginPage(ctx context.Context, tenantID int64, q LoginQuery) (Page[LoginLog], error) {
	where, args := loginFilter(tenantID, q)
	return queryPage(ctx, m.DB, `SELECT COUNT(*) FROM system_login_log `+where, `SELECT id, log_type, IFNULL(trace_id,''), user_id, user_type, IFNULL(username,''), result, IFNULL(user_ip,''), IFNULL(user_agent,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_login_log `+where+` ORDER BY id DESC`, args, q.PageNo, q.PageSize, scanLogin)
}

func (m *MySQL) LoginByID(ctx context.Context, tenantID, id int64) (*LoginLog, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT id, log_type, IFNULL(trace_id,''), user_id, user_type, IFNULL(username,''), result, IFNULL(user_ip,''), IFNULL(user_agent,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
		FROM system_login_log WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	item, err := scanLogin(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

func (m *MySQL) CreateOperate(ctx context.Context, tenantID int64, item OperateLog) error {
	_, err := m.DB.ExecContext(ctx, `INSERT INTO system_operate_log
		(trace_id, user_id, user_type, type, sub_type, biz_id, action, success, extra, request_method, request_url, user_ip, user_agent, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, 0, ?)`,
		cut(item.TraceID, 64), item.UserID, item.UserType, cut(item.Type, 50), cut(item.SubType, 50), item.BizID,
		cut(item.Action, 2000), cut(item.Extra, 2000), cut(item.RequestMethod, 16), cut(item.RequestURL, 255),
		cut(item.UserIP, 50), cut(item.UserAgent, 512), tenantID, time.Now())
	return err
}

func (m *MySQL) OperatePage(ctx context.Context, tenantID int64, q OperateQuery) (Page[OperateLog], error) {
	where, args := operateFilter(tenantID, q)
	from := ` FROM system_operate_log o LEFT JOIN system_users u ON u.id=o.user_id AND u.deleted=0 ` + where
	return queryPage(ctx, m.DB, `SELECT COUNT(*)`+from, `SELECT o.id, IFNULL(o.trace_id,''), o.user_id, IFNULL(u.nickname, IFNULL(u.username,'')), o.user_type, o.type, o.sub_type, o.biz_id, IFNULL(o.action,''), IFNULL(o.extra,''), IFNULL(o.request_method,''), IFNULL(o.request_url,''), IFNULL(o.user_ip,''), IFNULL(o.user_agent,''), IFNULL(UNIX_TIMESTAMP(o.create_time),0)*1000`+from+` ORDER BY o.id DESC`, args, q.PageNo, q.PageSize, scanOperate)
}

func (m *MySQL) OperateByID(ctx context.Context, tenantID, id int64) (*OperateLog, error) {
	row := m.DB.QueryRowContext(ctx, `SELECT o.id, IFNULL(o.trace_id,''), o.user_id, IFNULL(u.nickname, IFNULL(u.username,'')), o.user_type, o.type, o.sub_type, o.biz_id, IFNULL(o.action,''), IFNULL(o.extra,''), IFNULL(o.request_method,''), IFNULL(o.request_url,''), IFNULL(o.user_ip,''), IFNULL(o.user_agent,''), IFNULL(UNIX_TIMESTAMP(o.create_time),0)*1000
		FROM system_operate_log o LEFT JOIN system_users u ON u.id=o.user_id AND u.deleted=0
		WHERE o.id=? AND o.tenant_id=? AND o.deleted=0`, id, tenantID)
	item, err := scanOperate(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLogin(row scanner) (*LoginLog, error) {
	var item LoginLog
	err := row.Scan(&item.ID, &item.LogType, &item.TraceID, &item.UserID, &item.UserType, &item.Username, &item.Result, &item.UserIP, &item.UserAgent, &item.CreateTime)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanOperate(row scanner) (*OperateLog, error) {
	var item OperateLog
	err := row.Scan(&item.ID, &item.TraceID, &item.UserID, &item.UserName, &item.UserType, &item.Type, &item.SubType, &item.BizID, &item.Action, &item.Extra, &item.RequestMethod, &item.RequestURL, &item.UserIP, &item.UserAgent, &item.CreateTime)
	if err != nil {
		return nil, err
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

func cut(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
