package directory

import (
	"context"
	"database/sql"
	"time"
)

func (m *MySQL) DictTypeByID(ctx context.Context, id int64) (*DictType, error) {
	return scanDictType(m.DB.QueryRowContext(ctx, `SELECT id, name, type, status, IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_type WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) DictTypeByType(ctx context.Context, typ string) (*DictType, error) {
	return scanDictType(m.DB.QueryRowContext(ctx, `SELECT id, name, type, status, IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_type WHERE type=? AND deleted=0`, typ))
}

func scanDictType(row *sql.Row) (*DictType, error) {
	var item DictType
	var created sql.NullInt64
	err := row.Scan(&item.ID, &item.Name, &item.Type, &item.Status, &item.Remark, &created)
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

func (m *MySQL) DictTypeNameTaken(ctx context.Context, name string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_type WHERE deleted=0 AND name=? AND id<>?`, name, exceptID))
}

func (m *MySQL) DictTypeTaken(ctx context.Context, typ string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_type WHERE deleted=0 AND type=? AND id<>?`, typ, exceptID))
}

func (m *MySQL) DictTypeDataCount(ctx context.Context, typ string) (int, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_data WHERE deleted=0 AND dict_type=?`, typ).Scan(&n)
	return n, err
}

func (m *MySQL) CreateDictType(ctx context.Context, item DictType) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_dict_type (name, type, status, remark, deleted, create_time) VALUES (?, ?, ?, ?, 0, ?)`,
		item.Name, item.Type, item.Status, item.Remark, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateDictType(ctx context.Context, item DictType, previousType string) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE system_dict_type SET name=?, type=?, status=?, remark=? WHERE id=? AND deleted=0`,
		item.Name, item.Type, item.Status, item.Remark, item.ID); err != nil {
		return err
	}
	if previousType != "" && previousType != item.Type {
		if _, err := tx.ExecContext(ctx, `UPDATE system_dict_data SET dict_type=? WHERE dict_type=? AND deleted=0`, item.Type, previousType); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *MySQL) DeleteDictType(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_dict_type SET deleted=1, deleted_time=? WHERE id=? AND deleted=0`, time.Now(), id)
	return err
}

// DeleteDictTypeList 先锁定整批字典类型，再校验子项；任一类型有数据则回滚整批。
// 不存在的编号与 Java selectByIds/deleteByIds 一样被忽略。
func (m *MySQL) DeleteDictTypeList(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, type FROM system_dict_type WHERE deleted=0 AND id IN (`+rpcPlaces(len(ids))+`) ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return err
	}
	types := make([]string, 0, len(ids))
	for rows.Next() {
		var id int64
		var typ string
		if err := rows.Scan(&id, &typ); err != nil {
			_ = rows.Close()
			return err
		}
		types = append(types, typ)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, typ := range types {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_data WHERE deleted=0 AND dict_type=?`, typ).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return &Error{Code: 1_002_006_005, Msg: "无法删除，该字典类型还有字典数据"}
		}
	}
	args = append([]any{time.Now()}, args...)
	if _, err := tx.ExecContext(ctx, `UPDATE system_dict_type SET deleted=1, deleted_time=? WHERE deleted=0 AND id IN (`+rpcPlaces(len(ids))+`)`, args...); err != nil {
		return err
	}
	return tx.Commit()
}

func dictTypeWhere(query DictTypeQuery) (string, []any) {
	where := `WHERE deleted=0`
	var args []any
	if query.Name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+query.Name+"%")
	}
	if query.Type != "" {
		where += ` AND type LIKE ?`
		args = append(args, "%"+query.Type+"%")
	}
	if query.Status != nil {
		where += ` AND status=?`
		args = append(args, *query.Status)
	}
	if query.CreateStart != nil {
		where += ` AND create_time>=?`
		args = append(args, query.CreateStart.Format(time.DateTime))
	}
	if query.CreateEnd != nil {
		where += ` AND create_time<=?`
		args = append(args, query.CreateEnd.Format(time.DateTime))
	}
	return where, args
}

func (m *MySQL) DictTypePage(ctx context.Context, query DictTypeQuery) (Page[DictType], error) {
	if query.PageNo < 1 || query.PageSize < 1 || query.PageSize > 200 {
		return Page[DictType]{}, &Error{Code: codeBadRequest, Msg: "字典类型分页参数不正确"}
	}
	where, args := dictTypeWhere(query)
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_type `+where, args...).Scan(&total); err != nil {
		return Page[DictType]{}, err
	}
	args = append(args, query.PageSize, (query.PageNo-1)*query.PageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, type, status, IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_type `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[DictType]{}, err
	}
	defer rows.Close()
	list := make([]DictType, 0)
	for rows.Next() {
		var item DictType
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Status, &item.Remark, &created); err != nil {
			return Page[DictType]{}, err
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	return Page[DictType]{List: list, Total: total}, rows.Err()
}

// DictTypeExportRows 使用与分页一致的筛选和 id 倒序，逐行写入 XLSX。
func (m *MySQL) DictTypeExportRows(ctx context.Context, query DictTypeQuery, emit func(DictType) error) error {
	where, args := dictTypeWhere(query)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, type, status, IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_type `+where+` ORDER BY id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item DictType
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Status, &item.Remark, &created); err != nil {
			return err
		}
		if created.Valid {
			item.CreateTime = created.Int64
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (m *MySQL) DictTypeSimple(ctx context.Context) ([]DictType, error) {
	// Java 给字典数据编辑表单提供启用与停用类型，不能只返回启用项。
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, type, status, '', 0 FROM system_dict_type WHERE deleted=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DictType
	for rows.Next() {
		var item DictType
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.Status, &item.Remark, &item.CreateTime); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) DictDataByID(ctx context.Context, id int64) (*DictData, error) {
	var item DictData
	var created sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT id, sort, label, value, dict_type, status, IFNULL(color_type,''), IFNULL(css_class,''), IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_data WHERE id=? AND deleted=0`, id).
		Scan(&item.ID, &item.Sort, &item.Label, &item.Value, &item.DictType, &item.Status, &item.ColorType, &item.CSSClass, &item.Remark, &created)
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

func (m *MySQL) DictValueTaken(ctx context.Context, dictType, value string, exceptID int64) (bool, error) {
	return countAtLeastOne(m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_data WHERE deleted=0 AND dict_type=? AND value=? AND id<>?`, dictType, value, exceptID))
}

func (m *MySQL) CreateDictData(ctx context.Context, item DictData) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_dict_data
		(sort, label, value, dict_type, status, color_type, css_class, remark, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Sort, item.Label, item.Value, item.DictType, item.Status, item.ColorType, item.CSSClass, item.Remark, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateDictData(ctx context.Context, item DictData) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_dict_data SET sort=?, label=?, value=?, dict_type=?, status=?, color_type=?, css_class=?, remark=? WHERE id=? AND deleted=0`,
		item.Sort, item.Label, item.Value, item.DictType, item.Status, item.ColorType, item.CSSClass, item.Remark, item.ID)
	return err
}

func (m *MySQL) DeleteDictData(ctx context.Context, id int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_dict_data SET deleted=1 WHERE id=? AND deleted=0`, id)
	return err
}

// DeleteDictDataList 一条 SQL 批量软删，不存在的编号静默跳过。
func (m *MySQL) DeleteDictDataList(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE system_dict_data SET deleted=1 WHERE deleted=0 AND id IN (`+rpcPlaces(len(ids))+`)`, args...)
	return err
}

func dictDataWhere(label, dictType string, status *int) (string, []any) {
	where := `WHERE deleted=0`
	var args []any
	if label != "" {
		where += ` AND label LIKE ?`
		args = append(args, "%"+label+"%")
	}
	if dictType != "" {
		where += ` AND dict_type=?`
		args = append(args, dictType)
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	return where, args
}

// DictDataPage 对齐 Java 字典数据页的筛选和 dict_type、sort 倒序。
func (m *MySQL) DictDataPage(ctx context.Context, query DictDataQuery) (Page[DictData], error) {
	if query.PageNo < 1 || query.PageSize < 1 || query.PageSize > 200 {
		return Page[DictData]{}, &Error{Code: codeBadRequest, Msg: "字典数据分页参数不正确"}
	}
	where, args := dictDataWhere(query.Label, query.DictType, query.Status)
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_dict_data `+where, args...).Scan(&total); err != nil {
		return Page[DictData]{}, err
	}
	args = append(args, query.PageSize, (query.PageNo-1)*query.PageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, sort, label, value, dict_type, status, IFNULL(color_type,''), IFNULL(css_class,''), IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_data `+where+` ORDER BY dict_type DESC, sort DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[DictData]{}, err
	}
	defer rows.Close()
	result := Page[DictData]{List: make([]DictData, 0), Total: total}
	for rows.Next() {
		item, err := scanDictData(rows)
		if err != nil {
			return Page[DictData]{}, err
		}
		result.List = append(result.List, item)
	}
	return result, rows.Err()
}

// DictDataExportRows 复用分页筛选，流式遍历全部匹配项。
func (m *MySQL) DictDataExportRows(ctx context.Context, label, dictType string, status *int, emit func(DictData) error) error {
	where, args := dictDataWhere(label, dictType, status)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, sort, label, value, dict_type, status, IFNULL(color_type,''), IFNULL(css_class,''), IFNULL(remark,''), UNIX_TIMESTAMP(create_time)*1000
		FROM system_dict_data `+where+` ORDER BY dict_type DESC, sort DESC, id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanDictData(rows)
		if err != nil {
			return err
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanDictData(row interface{ Scan(...any) error }) (DictData, error) {
	var item DictData
	var created sql.NullInt64
	err := row.Scan(&item.ID, &item.Sort, &item.Label, &item.Value, &item.DictType, &item.Status,
		&item.ColorType, &item.CSSClass, &item.Remark, &created)
	if created.Valid {
		item.CreateTime = created.Int64
	}
	return item, err
}

// RPCDictDataList 对齐 Java getDictDataListByDictType：只排除逻辑删除，保留禁用项。
// system_dict_data 标记了 @TenantIgnore，因此查询不拼 tenant_id 条件。
func (m *MySQL) RPCDictDataList(ctx context.Context, dictType string) ([]RPCDictData, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT label, value, dict_type, status
		FROM system_dict_data WHERE dict_type=? AND deleted=0 ORDER BY sort, id`, dictType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRPCDictData(rows)
}

// RPCDictDataValues 对齐 Java selectByDictTypeAndValues，分批避免超长 IN 参数。
func (m *MySQL) RPCDictDataValues(ctx context.Context, dictType string, values []string) ([]RPCDictData, error) {
	result := make([]RPCDictData, 0)
	const batchSize = 500
	for start := 0; start < len(values); start += batchSize {
		end := start + batchSize
		if end > len(values) {
			end = len(values)
		}
		args := make([]any, 0, end-start+1)
		args = append(args, dictType)
		for _, value := range values[start:end] {
			args = append(args, value)
		}
		rows, err := m.DB.QueryContext(ctx, `SELECT label, value, dict_type, status
			FROM system_dict_data WHERE dict_type=? AND deleted=0 AND value IN (`+rpcPlaces(end-start)+`)`, args...)
		if err != nil {
			return nil, err
		}
		batch, scanErr := scanRPCDictData(rows)
		closeErr := rows.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		result = append(result, batch...)
	}
	return result, nil
}

func scanRPCDictData(rows *sql.Rows) ([]RPCDictData, error) {
	list := make([]RPCDictData, 0)
	for rows.Next() {
		var item RPCDictData
		if err := rows.Scan(&item.Label, &item.Value, &item.DictType, &item.Status); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}
