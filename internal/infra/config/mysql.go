package config

import (
	"context"
	"database/sql"
	"time"
)

// MySQL 读写 infra_config。
type MySQL struct {
	DB *sql.DB
}

const configSelect = `SELECT id, category, name, config_key, value, type, visible+0, IFNULL(remark,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM infra_config`

func (m *MySQL) ByID(ctx context.Context, id int64) (*Item, error) {
	return scanItem(m.DB.QueryRowContext(ctx, configSelect+` WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) ByKey(ctx context.Context, key string) (*Item, error) {
	return scanItem(m.DB.QueryRowContext(ctx, configSelect+` WHERE config_key=? AND deleted=0`, key))
}

func scanItem(row *sql.Row) (*Item, error) {
	var item Item
	var visible int
	err := row.Scan(&item.ID, &item.Category, &item.Name, &item.Key, &item.Value, &item.Type, &visible, &item.Remark, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Visible = visible == 1
	return &item, nil
}

func (m *MySQL) Page(ctx context.Context, pageNo, pageSize int, name, key string, typ *int, start, end *int64) (Page[Item], error) {
	where := `WHERE deleted=0`
	var args []any
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if key != "" {
		where += ` AND config_key LIKE ?`
		args = append(args, "%"+key+"%")
	}
	if typ != nil {
		where += ` AND type=?`
		args = append(args, *typ)
	}
	if start != nil {
		where += ` AND create_time>=FROM_UNIXTIME(?/1000)`
		args = append(args, *start)
	}
	if end != nil {
		where += ` AND create_time<=FROM_UNIXTIME(?/1000)`
		args = append(args, *end)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_config `+where, args...).Scan(&total); err != nil {
		return Page[Item]{}, err
	}
	pageNo, pageSize, all := normalize(pageNo, pageSize)
	listArgs := append([]any{}, args...)
	query := configSelect + ` ` + where + ` ORDER BY id DESC`
	if all {
		query += ` LIMIT ?`
		listArgs = append(listArgs, pageSize)
	} else {
		query += ` LIMIT ? OFFSET ?`
		listArgs = append(listArgs, pageSize, (pageNo-1)*pageSize)
	}
	rows, err := m.DB.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return Page[Item]{}, err
	}
	defer rows.Close()
	list := make([]Item, 0)
	for rows.Next() {
		var item Item
		var visible int
		if err := rows.Scan(&item.ID, &item.Category, &item.Name, &item.Key, &item.Value, &item.Type, &visible, &item.Remark, &item.CreateTime); err != nil {
			return Page[Item]{}, err
		}
		item.Visible = visible == 1
		list = append(list, item)
	}
	return Page[Item]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) Create(ctx context.Context, item Item) (int64, error) {
	visible := 0
	if item.Visible {
		visible = 1
	}
	res, err := m.DB.ExecContext(ctx, `INSERT INTO infra_config (category, type, name, config_key, value, visible, remark, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Category, item.Type, item.Name, item.Key, item.Value, visible, item.Remark, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) Update(ctx context.Context, item Item) error {
	visible := 0
	if item.Visible {
		visible = 1
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_config SET category=?, name=?, config_key=?, value=?, visible=?, remark=? WHERE id=? AND deleted=0`,
		item.Category, item.Name, item.Key, item.Value, visible, item.Remark, item.ID)
	return err
}

func (m *MySQL) Delete(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_config SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

func normalize(pageNo, pageSize int) (int, int, bool) {
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
