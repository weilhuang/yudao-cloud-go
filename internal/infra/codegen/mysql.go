package codegen

import (
	"context"
	"database/sql"
	"strings"
)

const tableSelect = `SELECT id, data_source_config_id, scene, table_name, IFNULL(table_comment,''), IFNULL(remark,''),
	module_name, business_name, class_name, class_comment, author, template_type, front_type,
	parent_menu_id, master_table_id, sub_join_column_id, sub_join_many,
	tree_parent_column_id, tree_name_column_id,
	UNIX_TIMESTAMP(create_time)*1000, UNIX_TIMESTAMP(update_time)*1000
	FROM infra_codegen_table`

func scanTable(row interface{ Scan(...any) error }) (*Table, error) {
	var item Table
	var parent, master, subJoin, treeParent, treeName sql.NullInt64
	var many []byte
	var created, updated sql.NullInt64
	err := row.Scan(&item.ID, &item.DataSourceConfigID, &item.Scene, &item.TableName, &item.TableComment, &item.Remark,
		&item.ModuleName, &item.BusinessName, &item.ClassName, &item.ClassComment, &item.Author,
		&item.TemplateType, &item.FrontType, &parent, &master, &subJoin, &many, &treeParent, &treeName, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.ParentMenuID = nullInt(parent)
	item.MasterTableID = nullInt(master)
	item.SubJoinColumnID = nullInt(subJoin)
	item.SubJoinMany = bitPtr(many)
	item.TreeParentColumnID = nullInt(treeParent)
	item.TreeNameColumnID = nullInt(treeName)
	if created.Valid {
		item.CreateTime = created.Int64
	}
	if updated.Valid {
		item.UpdateTime = updated.Int64
	}
	return &item, nil
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func bitPtr(b []byte) *bool {
	if b == nil {
		return nil
	}
	v := len(b) > 0 && b[0] != 0
	return &v
}

func bitArg(v *bool) any {
	if v == nil {
		return nil
	}
	if *v {
		return 1
	}
	return 0
}

func (s *Service) tableByID(ctx context.Context, id int64) (*Table, error) {
	return scanTable(s.DB.QueryRowContext(ctx, tableSelect+` WHERE id=? AND deleted=0`, id))
}

func (s *Service) tableByName(ctx context.Context, dataSourceID int64, name string) (*Table, error) {
	return scanTable(s.DB.QueryRowContext(ctx, tableSelect+` WHERE table_name=? AND data_source_config_id=? AND deleted=0`, name, dataSourceID))
}

func (s *Service) tablesBySource(ctx context.Context, dataSourceID int64) ([]Table, error) {
	rows, err := s.DB.QueryContext(ctx, tableSelect+` WHERE data_source_config_id=? AND deleted=0 ORDER BY id`, dataSourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTables(rows)
}

func (s *Service) subTables(ctx context.Context, masterID int64) ([]Table, error) {
	rows, err := s.DB.QueryContext(ctx, tableSelect+` WHERE template_type=? AND master_table_id=? AND deleted=0 ORDER BY id`, templateSub, masterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTables(rows)
}

func scanTables(rows *sql.Rows) ([]Table, error) {
	var list []Table
	for rows.Next() {
		item, err := scanTable(rows)
		if err != nil {
			return nil, err
		}
		if item != nil {
			list = append(list, *item)
		}
	}
	if list == nil {
		list = []Table{}
	}
	return list, rows.Err()
}

func (s *Service) columnsByTable(ctx context.Context, tableID int64) ([]Column, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, table_id, column_name, data_type, column_comment, nullable, primary_key,
		ordinal_position, java_type, java_field, IFNULL(dict_type,''), IFNULL(example,''),
		create_operation, update_operation, list_operation, list_operation_condition, list_operation_result, html_type,
		UNIX_TIMESTAMP(create_time)*1000
		FROM infra_codegen_column WHERE table_id=? AND deleted=0 ORDER BY ordinal_position, id`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Column
	for rows.Next() {
		var item Column
		var nullable, primary, createOp, updateOp, listOp, listResult []byte
		var created sql.NullInt64
		if err := rows.Scan(&item.ID, &item.TableID, &item.ColumnName, &item.DataType, &item.ColumnComment,
			&nullable, &primary, &item.OrdinalPosition, &item.JavaType, &item.JavaField, &item.DictType, &item.Example,
			&createOp, &updateOp, &listOp, &item.ListOperationCondition, &listResult, &item.HTMLType, &created); err != nil {
			return nil, err
		}
		item.Nullable = len(nullable) > 0 && nullable[0] != 0
		item.PrimaryKey = len(primary) > 0 && primary[0] != 0
		item.CreateOperation = len(createOp) > 0 && createOp[0] != 0
		item.UpdateOperation = len(updateOp) > 0 && updateOp[0] != 0
		item.ListOperation = len(listOp) > 0 && listOp[0] != 0
		item.ListOperationResult = len(listResult) > 0 && listResult[0] != 0
		if created.Valid {
			item.CreateTime = created.Int64
		}
		list = append(list, item)
	}
	if list == nil {
		list = []Column{}
	}
	return list, rows.Err()
}

func (s *Service) insertTable(ctx context.Context, tx *sql.Tx, table Table) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO infra_codegen_table
		(data_source_config_id, scene, table_name, table_comment, remark, module_name, business_name, class_name, class_comment,
		 author, template_type, front_type, parent_menu_id, master_table_id, sub_join_column_id, sub_join_many,
		 tree_parent_column_id, tree_name_column_id, deleted)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`,
		table.DataSourceConfigID, table.Scene, table.TableName, table.TableComment, nullString(table.Remark),
		table.ModuleName, table.BusinessName, table.ClassName, table.ClassComment, table.Author,
		table.TemplateType, table.FrontType, table.ParentMenuID, table.MasterTableID, table.SubJoinColumnID, bitArg(table.SubJoinMany),
		table.TreeParentColumnID, table.TreeNameColumnID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) insertColumns(ctx context.Context, tx *sql.Tx, columns []Column) error {
	if len(columns) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`INSERT INTO infra_codegen_column
		(table_id, column_name, data_type, column_comment, nullable, primary_key, ordinal_position, java_type, java_field,
		 dict_type, example, create_operation, update_operation, list_operation, list_operation_condition, list_operation_result, html_type, deleted)
		VALUES `)
	args := make([]any, 0, len(columns)*17)
	for i, column := range columns {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)")
		example := any(column.Example)
		if column.Example == "" {
			example = nil
		}
		args = append(args, column.TableID, column.ColumnName, column.DataType, column.ColumnComment,
			boolArg(column.Nullable), boolArg(column.PrimaryKey), column.OrdinalPosition, column.JavaType, column.JavaField,
			column.DictType, example, boolArg(column.CreateOperation), boolArg(column.UpdateOperation), boolArg(column.ListOperation),
			column.ListOperationCondition, boolArg(column.ListOperationResult), column.HTMLType)
	}
	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func boolArg(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *Service) pageTables(ctx context.Context, q PageQuery) (Page, error) {
	where, args := pageWhere(q)
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_codegen_table WHERE deleted=0`+where, args...).Scan(&total); err != nil {
		return Page{}, err
	}
	query := tableSelect + ` WHERE deleted=0` + where + ` ORDER BY update_time DESC, id DESC`
	pageArgs := append([]any{}, args...)
	if q.PageSize > 0 {
		if q.PageNo <= 0 {
			q.PageNo = 1
		}
		query += ` LIMIT ? OFFSET ?`
		pageArgs = append(pageArgs, q.PageSize, (q.PageNo-1)*q.PageSize)
	}
	rows, err := s.DB.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	list, err := scanTables(rows)
	if err != nil {
		return Page{}, err
	}
	return Page{List: list, Total: total}, nil
}

func pageWhere(q PageQuery) (string, []any) {
	var b strings.Builder
	var args []any
	if q.TableName != "" {
		b.WriteString(` AND table_name LIKE ?`)
		args = append(args, "%"+q.TableName+"%")
	}
	if q.TableComment != "" {
		b.WriteString(` AND table_comment LIKE ?`)
		args = append(args, "%"+q.TableComment+"%")
	}
	if q.ClassName != "" {
		b.WriteString(` AND class_name LIKE ?`)
		args = append(args, "%"+q.ClassName+"%")
	}
	if q.CreateFrom != "" && q.CreateTo != "" {
		b.WriteString(` AND create_time BETWEEN ? AND ?`)
		args = append(args, q.CreateFrom, q.CreateTo)
	}
	return b.String(), args
}

func (s *Service) updateTable(ctx context.Context, tx *sql.Tx, table Table) error {
	_, err := tx.ExecContext(ctx, `UPDATE infra_codegen_table SET scene=?, table_name=?, table_comment=?, remark=?,
		module_name=?, business_name=?, class_name=?, class_comment=?, author=?, template_type=?, front_type=?,
		parent_menu_id=?, master_table_id=?, sub_join_column_id=?, sub_join_many=?, tree_parent_column_id=?, tree_name_column_id=?
		WHERE id=? AND deleted=0`,
		table.Scene, table.TableName, table.TableComment, nullString(table.Remark), table.ModuleName, table.BusinessName,
		table.ClassName, table.ClassComment, table.Author, table.TemplateType, table.FrontType,
		table.ParentMenuID, table.MasterTableID, table.SubJoinColumnID, bitArg(table.SubJoinMany),
		table.TreeParentColumnID, table.TreeNameColumnID, table.ID)
	return err
}

func (s *Service) updateColumn(ctx context.Context, tx *sql.Tx, column Column) error {
	example := any(column.Example)
	if column.Example == "" {
		example = nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE infra_codegen_column SET table_id=?, column_name=?, data_type=?, column_comment=?,
		nullable=?, primary_key=?, ordinal_position=?, java_type=?, java_field=?, dict_type=?, example=?,
		create_operation=?, update_operation=?, list_operation=?, list_operation_condition=?, list_operation_result=?, html_type=?
		WHERE id=? AND deleted=0`,
		column.TableID, column.ColumnName, column.DataType, column.ColumnComment, boolArg(column.Nullable), boolArg(column.PrimaryKey),
		column.OrdinalPosition, column.JavaType, column.JavaField, column.DictType, example,
		boolArg(column.CreateOperation), boolArg(column.UpdateOperation), boolArg(column.ListOperation),
		column.ListOperationCondition, boolArg(column.ListOperationResult), column.HTMLType, column.ID)
	return err
}

func (s *Service) softDelete(ctx context.Context, tx *sql.Tx, tableIDs []int64) error {
	if len(tableIDs) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(tableIDs)), ",")
	args := make([]any, len(tableIDs))
	for i, id := range tableIDs {
		args[i] = id
	}
	if _, err := tx.ExecContext(ctx, `UPDATE infra_codegen_table SET deleted=1 WHERE deleted=0 AND id IN (`+marks+`)`, args...); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE infra_codegen_column SET deleted=1 WHERE deleted=0 AND table_id IN (`+marks+`)`, args...)
	return err
}

func (s *Service) softDeleteColumns(ctx context.Context, tx *sql.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := tx.ExecContext(ctx, `UPDATE infra_codegen_column SET deleted=1 WHERE deleted=0 AND id IN (`+marks+`)`, args...)
	return err
}

func (s *Service) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
