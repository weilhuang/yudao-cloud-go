package datasource

import (
	"context"
	"database/sql"
	"strings"
)

// MySQL 读写数据源配置。密码列保存的是密文。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) Insert(ctx context.Context, row Row) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO infra_data_source_config
		(name, url, username, password, deleted) VALUES (?, ?, ?, ?, 0)`,
		row.Name, row.URL, row.Username, row.Password)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) Update(ctx context.Context, row Row) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_data_source_config
		SET name=?, url=?, username=?, password=? WHERE id=? AND deleted=0`,
		row.Name, row.URL, row.Username, row.Password, row.ID)
	return err
}

func (m *MySQL) Get(ctx context.Context, id int64) (*Row, error) {
	var row Row
	var created sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT id, name, url, username, password, UNIX_TIMESTAMP(create_time)*1000
		FROM infra_data_source_config WHERE id=? AND deleted=0`, id).
		Scan(&row.ID, &row.Name, &row.URL, &row.Username, &row.Password, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if created.Valid {
		row.CreateTime = created.Int64
	}
	return &row, nil
}

func (m *MySQL) List(ctx context.Context) ([]Row, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, url, username, password, UNIX_TIMESTAMP(create_time)*1000
		FROM infra_data_source_config WHERE deleted=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]Row, 0)
	for rows.Next() {
		var row Row
		var created sql.NullInt64
		if err := rows.Scan(&row.ID, &row.Name, &row.URL, &row.Username, &row.Password, &created); err != nil {
			return nil, err
		}
		if created.Valid {
			row.CreateTime = created.Int64
		}
		list = append(list, row)
	}
	return list, rows.Err()
}

func (m *MySQL) Delete(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_data_source_config SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

func (m *MySQL) DeleteList(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_data_source_config SET deleted=1 WHERE deleted=0 AND id IN (`+marks+`)`, args...)
	return err
}
