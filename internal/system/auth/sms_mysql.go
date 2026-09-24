package auth

import (
	"context"
	"database/sql"
	"time"
)

func (m *MySQL) FindByMobile(ctx context.Context, tenantID int64, mobile string) (*User, error) {
	query := `SELECT id, tenant_id, username, password, nickname, IFNULL(avatar,''), IFNULL(email,''), dept_id, status
		FROM system_users WHERE mobile = ? AND deleted = 0`
	args := []any{mobile}
	if tenantID > 0 {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` ORDER BY id LIMIT 1`
	return m.scanUser(m.DB.QueryRowContext(ctx, query, args...))
}

func (m *MySQL) CountUsers(ctx context.Context, tenantID int64) (int64, error) {
	var n int64
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_users WHERE deleted=0 AND tenant_id=?`, tenantID).Scan(&n)
	return n, err
}

func (m *MySQL) AccountLimit(ctx context.Context, tenantID int64) (int64, error) {
	var n int64
	err := m.DB.QueryRowContext(ctx, `SELECT account_count FROM system_tenant WHERE id=? AND deleted=0`, tenantID).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

func (m *MySQL) ConfigValue(ctx context.Context, key string) (string, error) {
	var value sql.NullString
	err := m.DB.QueryRowContext(ctx, `SELECT value FROM infra_config WHERE config_key=? AND deleted=0 LIMIT 1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value.String, err
}

func (m *MySQL) RegisterUser(ctx context.Context, tenantID int64, username, nickname, passwordHash string) (*User, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_users
		(username, password, nickname, status, post_ids, deleted, tenant_id, create_time)
		VALUES (?, ?, ?, 0, '[]', 0, ?, ?)`, username, passwordHash, nickname, tenantID, time.Now())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, TenantID: tenantID, Username: username, Nickname: nickname, Password: passwordHash, Status: statusEnable}, nil
}

func (m *MySQL) UpdatePassword(ctx context.Context, tenantID, userID int64, passwordHash string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_users SET password=? WHERE id=? AND tenant_id=? AND deleted=0`, passwordHash, userID, tenantID)
	return err
}

func (m *MySQL) LastSmsCode(ctx context.Context, mobile, code string, scene int, filterCode, filterScene bool) (*SmsCode, error) {
	query := `SELECT id, mobile, code, scene, today_index, create_time, used+0, tenant_id
		FROM system_sms_code WHERE mobile=? AND deleted=0`
	args := []any{mobile}
	if filterScene {
		query += ` AND scene=?`
		args = append(args, scene)
	}
	if filterCode {
		query += ` AND code=?`
		args = append(args, code)
	}
	query += ` ORDER BY id DESC LIMIT 1`
	var item SmsCode
	var used int
	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&item.ID, &item.Mobile, &item.Code, &item.Scene, &item.TodayIndex, &item.CreateTime, &used, &item.TenantID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Used = used != 0
	return &item, nil
}

func (m *MySQL) InsertSmsCode(ctx context.Context, item SmsCode, ip string) error {
	_, err := m.DB.ExecContext(ctx, `INSERT INTO system_sms_code
		(mobile, code, scene, create_ip, today_index, used, create_time, deleted, tenant_id)
		VALUES (?, ?, ?, ?, ?, 0, ?, 0, ?)`,
		item.Mobile, item.Code, item.Scene, ip, item.TodayIndex, item.CreateTime, item.TenantID)
	return err
}

func (m *MySQL) UseSmsCode(ctx context.Context, id int64, ip string, at time.Time) error {
	res, err := m.DB.ExecContext(ctx, `UPDATE system_sms_code SET used=1, used_time=?, used_ip=? WHERE id=? AND used=0`, at, ip, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return &Error{Code: codeSmsUsed, Msg: "验证码已使用"}
	}
	return nil
}

func (m *MySQL) DiscardSmsCode(ctx context.Context, mobile, code string, scene int) error {
	_, err := m.DB.ExecContext(ctx, `DELETE FROM system_sms_code
		WHERE mobile=? AND code=? AND scene=? AND used=0 ORDER BY id DESC LIMIT 1`, mobile, code, scene)
	return err
}
