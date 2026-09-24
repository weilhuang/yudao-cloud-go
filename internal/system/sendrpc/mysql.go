package sendrpc

import (
	"context"
	"database/sql"
)

// MySQL 只查管理员的手机和邮箱，不套数据权限。会员资料不在 system 表里。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) AdminMobile(ctx context.Context, tenantID, userID int64) (string, error) {
	return m.adminField(ctx, "mobile", tenantID, userID)
}

func (m *MySQL) AdminEmail(ctx context.Context, tenantID, userID int64) (string, error) {
	return m.adminField(ctx, "email", tenantID, userID)
}

func (m *MySQL) adminField(ctx context.Context, column string, tenantID, userID int64) (string, error) {
	var value sql.NullString
	err := m.DB.QueryRowContext(ctx, `SELECT `+column+` FROM system_users WHERE id=? AND tenant_id=? AND deleted=0`, userID, tenantID).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value.String, nil
}
