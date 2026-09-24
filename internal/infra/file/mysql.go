package file

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// MySQL 读写 infra_file、infra_file_config 和 infra_file_content。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) Master(ctx context.Context) (*Config, error) {
	return m.scanConfig(m.DB.QueryRowContext(ctx, configSelect+` WHERE deleted=0 AND master=1 ORDER BY id DESC LIMIT 1`))
}

func (m *MySQL) ConfigByID(ctx context.Context, id int64) (*Config, error) {
	return m.scanConfig(m.DB.QueryRowContext(ctx, configSelect+` WHERE id=? AND deleted=0`, id))
}

const configSelect = `SELECT id, name, storage, master+0, config, IFNULL(remark,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM infra_file_config`

func (m *MySQL) scanConfig(row *sql.Row) (*Config, error) {
	var item Config
	var master int
	var raw string
	err := row.Scan(&item.ID, &item.Name, &item.Storage, &master, &raw, &item.Remark, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Master = master == 1
	item.Config = json.RawMessage(raw)
	return &item, nil
}

func (m *MySQL) ConfigPage(ctx context.Context, pageNo, pageSize int, name string, storage *int) (Page[Config], error) {
	where := `WHERE deleted=0`
	var args []any
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if storage != nil {
		where += ` AND storage=?`
		args = append(args, *storage)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_file_config `+where, args...).Scan(&total); err != nil {
		return Page[Config]{}, err
	}
	pageNo, pageSize = normalize(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, configSelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[Config]{}, err
	}
	defer rows.Close()
	list := make([]Config, 0)
	for rows.Next() {
		var item Config
		var master int
		var raw string
		if err := rows.Scan(&item.ID, &item.Name, &item.Storage, &master, &raw, &item.Remark, &item.CreateTime); err != nil {
			return Page[Config]{}, err
		}
		item.Master = master == 1
		item.Config = json.RawMessage(raw)
		list = append(list, item)
	}
	return Page[Config]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) CreateConfig(ctx context.Context, item Config) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO infra_file_config (name, storage, remark, master, config, deleted, create_time) VALUES (?, ?, ?, 0, ?, 0, ?)`,
		item.Name, item.Storage, item.Remark, string(item.Config), time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateConfig(ctx context.Context, item Config) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_file_config SET name=?, storage=?, remark=?, config=? WHERE id=? AND deleted=0`,
		item.Name, item.Storage, item.Remark, string(item.Config), item.ID)
	return err
}

func (m *MySQL) UpdateMaster(ctx context.Context, id int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE infra_file_config SET master=0 WHERE deleted=0`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE infra_file_config SET master=1 WHERE id=? AND deleted=0`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *MySQL) DeleteConfig(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_file_config SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

func (m *MySQL) InsertFile(ctx context.Context, item Item) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO infra_file (config_id, name, path, url, type, size, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		item.ConfigID, item.Name, item.Path, item.URL, item.Type, item.Size, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) FilePage(ctx context.Context, pageNo, pageSize int, objectPath, mime string) (Page[Item], error) {
	where := `WHERE deleted=0`
	var args []any
	if objectPath != "" {
		where += ` AND path LIKE ?`
		args = append(args, "%"+objectPath+"%")
	}
	if mime != "" {
		where += ` AND type LIKE ?`
		args = append(args, "%"+mime+"%")
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_file `+where, args...).Scan(&total); err != nil {
		return Page[Item]{}, err
	}
	pageNo, pageSize = normalize(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, config_id, IFNULL(name,''), path, url, IFNULL(type,''), size, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM infra_file `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[Item]{}, err
	}
	defer rows.Close()
	list := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.ConfigID, &item.Name, &item.Path, &item.URL, &item.Type, &item.Size, &item.CreateTime); err != nil {
			return Page[Item]{}, err
		}
		list = append(list, item)
	}
	return Page[Item]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) FileByID(ctx context.Context, id int64) (*Item, error) {
	return scanFile(m.DB.QueryRowContext(ctx, `SELECT id, config_id, IFNULL(name,''), path, url, IFNULL(type,''), size, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM infra_file WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) FileByPath(ctx context.Context, configID int64, objectPath string) (*Item, error) {
	return scanFile(m.DB.QueryRowContext(ctx, `SELECT id, config_id, IFNULL(name,''), path, url, IFNULL(type,''), size, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM infra_file WHERE config_id=? AND path=? AND deleted=0 ORDER BY id DESC LIMIT 1`, configID, objectPath))
}

func scanFile(row *sql.Row) (*Item, error) {
	var item Item
	err := row.Scan(&item.ID, &item.ConfigID, &item.Name, &item.Path, &item.URL, &item.Type, &item.Size, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *MySQL) DeleteFile(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_file SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

func (m *MySQL) SaveBlob(ctx context.Context, configID int64, objectPath string, content []byte) error {
	_, err := m.DB.ExecContext(ctx, `INSERT INTO infra_file_content (config_id, path, content, deleted, create_time) VALUES (?, ?, ?, 0, ?)`,
		configID, objectPath, content, time.Now())
	return err
}

func (m *MySQL) LoadBlob(ctx context.Context, configID int64, objectPath string) ([]byte, error) {
	var content []byte
	err := m.DB.QueryRowContext(ctx, `SELECT content FROM infra_file_content WHERE config_id=? AND path=? AND deleted=0 ORDER BY id DESC LIMIT 1`, configID, objectPath).Scan(&content)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return content, err
}

func (m *MySQL) DeleteBlob(ctx context.Context, configID int64, objectPath string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE infra_file_content SET deleted=1 WHERE config_id=? AND path=? AND deleted=0`, configID, objectPath)
	return err
}

func normalize(pageNo, pageSize int) (int, int) {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return pageNo, pageSize
}
