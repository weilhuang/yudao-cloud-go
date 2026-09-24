package demo02

import (
	"context"
	"database/sql"
	"time"
)

const categorySelect = `SELECT id, name, parent_id, UNIX_TIMESTAMP(create_time)*1000 FROM yudao_demo02_category`

func (s *Service) get(ctx context.Context, tenantID, id int64) (*Category, error) {
	var item Category
	var created sql.NullInt64
	err := s.DB.QueryRowContext(ctx, categorySelect+` WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID).
		Scan(&item.ID, &item.Name, &item.ParentID, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, nil
}

func (s *Service) byParentName(ctx context.Context, tenantID, parentID int64, name string) (*Category, error) {
	var item Category
	var created sql.NullInt64
	err := s.DB.QueryRowContext(ctx, categorySelect+` WHERE parent_id=? AND name=? AND tenant_id=? AND deleted=0`, parentID, name, tenantID).
		Scan(&item.ID, &item.Name, &item.ParentID, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return &item, nil
}

func (s *Service) childCount(ctx context.Context, tenantID, parentID int64) (int64, error) {
	var n int64
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM yudao_demo02_category WHERE parent_id=? AND tenant_id=? AND deleted=0`, parentID, tenantID).Scan(&n)
	return n, err
}

func (s *Service) insert(ctx context.Context, tenantID int64, in Save) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO yudao_demo02_category (name, parent_id, deleted, tenant_id) VALUES (?,?,0,?)`,
		in.Name, in.ParentID, tenantID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) update(ctx context.Context, tenantID int64, in Save) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE yudao_demo02_category SET name=?, parent_id=? WHERE id=? AND tenant_id=? AND deleted=0`,
		in.Name, in.ParentID, in.ID, tenantID)
	return err
}

func (s *Service) softDelete(ctx context.Context, tenantID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE yudao_demo02_category SET deleted=1 WHERE id=? AND tenant_id=? AND deleted=0`, id, tenantID)
	return err
}

func (s *Service) list(ctx context.Context, tenantID int64, q Query) ([]Category, error) {
	query := categorySelect + ` WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if q.Name != "" {
		query += ` AND name LIKE ?`
		args = append(args, "%"+q.Name+"%")
	}
	if q.ParentID != nil {
		query += ` AND parent_id=?`
		args = append(args, *q.ParentID)
	}
	if q.CreateFrom != "" && q.CreateTo != "" {
		query += ` AND create_time BETWEEN ? AND ?`
		args = append(args, q.CreateFrom, q.CreateTo)
	}
	query += ` ORDER BY id DESC`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Category{}
	for rows.Next() {
		var item Category
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Name, &item.ParentID, &created); err != nil {
			return nil, err
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func shanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*3600)
	}
	return loc
}
