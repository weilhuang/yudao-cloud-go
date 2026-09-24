package tenantrpc

import (
	"context"
	"database/sql"
)

// MySQL 只查询 system_tenant。该表在 Java 中标记 @TenantIgnore，没有 tenant_id 字段。
type MySQL struct{ DB *sql.DB }

func (m *MySQL) TenantIDs(ctx context.Context) ([]int64, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id FROM system_tenant WHERE deleted=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (m *MySQL) TenantByID(ctx context.Context, id int64) (*Tenant, error) {
	var tenant Tenant
	err := m.DB.QueryRowContext(ctx, `SELECT id, name, status, expire_time FROM system_tenant WHERE id=? AND deleted=0`, id).
		Scan(&tenant.ID, &tenant.Name, &tenant.Status, &tenant.ExpireTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}
