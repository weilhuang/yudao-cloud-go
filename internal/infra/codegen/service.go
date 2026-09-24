package codegen

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/weilhuang/yudao-cloud-go/internal/infra/datasource"
)

// Service 管理代码生成的表和字段定义。物理表可以来自主库，也可以来自已保存的数据源。
type Service struct {
	DB      *sql.DB
	Sources datasource.Store
	Key     string
	// Author 取当前管理员昵称，作为导入时的作者。为空时作者留空。
	Author func(ctx context.Context, userID int64) (string, error)
}

func (s *Service) CreateList(ctx context.Context, userID, dataSourceID int64, names []string) ([]int64, error) {
	author := ""
	if s.Author != nil {
		var err error
		author, err = s.Author(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	type built struct {
		table   Table
		columns []Column
	}
	items := make([]built, 0, len(names))
	for _, name := range names {
		schema, err := s.schema(ctx, dataSourceID, name)
		if err != nil {
			return nil, err
		}
		if err := validateSchema(schema); err != nil {
			return nil, err
		}
		exists, err := s.tableByName(ctx, dataSourceID, name)
		if err != nil {
			return nil, err
		}
		if exists != nil {
			return nil, biz(codeTableExists, "表定义已经存在")
		}
		hasPK := false
		for _, column := range schema.Columns {
			if column.PrimaryKey {
				hasPK = true
				break
			}
		}
		items = append(items, built{
			table:   buildTable(schema.Name, schema.Comment, author, dataSourceID),
			columns: buildColumns(0, schema.Columns, !hasPK),
		})
	}
	ids := make([]int64, 0, len(items))
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, item := range items {
			id, err := s.insertTable(ctx, tx, item.table)
			if err != nil {
				return err
			}
			for i := range item.columns {
				item.columns[i].TableID = id
			}
			if err := s.insertColumns(ctx, tx, item.columns); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Service) Update(ctx context.Context, table Table, columns []Column) error {
	current, err := s.tableByID(ctx, table.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeTableNotExists, "表定义不存在")
	}
	if table.TemplateType == templateSub {
		if table.MasterTableID == nil {
			return masterMissingErr(0)
		}
		master, err := s.tableByID(ctx, *table.MasterTableID)
		if err != nil {
			return err
		}
		if master == nil {
			return masterMissingErr(*table.MasterTableID)
		}
		found := false
		if table.SubJoinColumnID != nil {
			for _, column := range columns {
				if column.ID == *table.SubJoinColumnID {
					found = true
					break
				}
			}
		}
		if !found {
			id := int64(0)
			if table.SubJoinColumnID != nil {
				id = *table.SubJoinColumnID
			}
			return subColumnMissingErr(id)
		}
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := s.updateTable(ctx, tx, table); err != nil {
			return err
		}
		for _, column := range columns {
			if err := s.updateColumn(ctx, tx, column); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) Sync(ctx context.Context, tableID int64) error {
	table, err := s.tableByID(ctx, tableID)
	if err != nil {
		return err
	}
	if table == nil {
		return biz(codeTableNotExists, "表定义不存在")
	}
	schema, err := s.schema(ctx, table.DataSourceConfigID, table.TableName)
	if err != nil {
		return err
	}
	if err := validateSchema(schema); err != nil {
		return err
	}
	saved, err := s.columnsByTable(ctx, tableID)
	if err != nil {
		return err
	}
	inserts, deletes, none := syncPlan(saved, schema.Columns)
	if none {
		return biz(codeSyncNone, "同步失败，不存在改变")
	}
	hasPK := false
	for _, column := range inserts {
		if column.PrimaryKey {
			hasPK = true
			break
		}
	}
	fresh := buildColumns(tableID, inserts, len(inserts) > 0 && !hasPK)
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := s.insertColumns(ctx, tx, fresh); err != nil {
			return err
		}
		return s.softDeleteColumns(ctx, tx, deletes)
	})
}

func (s *Service) Delete(ctx context.Context, tableID int64) error {
	current, err := s.tableByID(ctx, tableID)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeTableNotExists, "表定义不存在")
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		return s.softDelete(ctx, tx, []int64{tableID})
	})
}

func (s *Service) DeleteList(ctx context.Context, tableIDs []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		return s.softDelete(ctx, tx, tableIDs)
	})
}

func (s *Service) List(ctx context.Context, dataSourceID int64) ([]Table, error) {
	return s.tablesBySource(ctx, dataSourceID)
}

func (s *Service) Page(ctx context.Context, q PageQuery) (Page, error) {
	if q.PageNo <= 0 {
		q.PageNo = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 10
	}
	return s.pageTables(ctx, q)
}

func (s *Service) Detail(ctx context.Context, tableID int64) (*Table, []Column, error) {
	table, err := s.tableByID(ctx, tableID)
	if err != nil || table == nil {
		return table, nil, err
	}
	columns, err := s.columnsByTable(ctx, tableID)
	return table, columns, err
}

func (s *Service) DatabaseTables(ctx context.Context, dataSourceID int64, name, comment string) ([]DBTable, error) {
	db, closeDB, err := s.catalog(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	defer closeDB()
	tables, err := listPhysicalTables(ctx, db, name, comment)
	if err != nil {
		return nil, err
	}
	imported, err := s.tablesBySource(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, table := range imported {
		exists[table.TableName] = true
	}
	out := make([]DBTable, 0, len(tables))
	for _, table := range tables {
		if !exists[table.Name] {
			out = append(out, table)
		}
	}
	return out, nil
}

func (s *Service) Preview(ctx context.Context, tableID int64) ([]PreviewFile, error) {
	table, columns, err := s.ready(ctx, tableID)
	if err != nil {
		return nil, err
	}
	if err := validateGenerationSupport(*table); err != nil {
		return nil, err
	}
	return previewFiles(*table, columns), nil
}

// 尚未移植的模板不能返回看似完整的代码包，避免生成可下载但缺失页面和子表逻辑的文件。
func validateGenerationSupport(table Table) error {
	if table.TemplateType != templateOne && table.TemplateType != templateTree {
		return biz(400, "当前尚不支持该模板类型的代码生成")
	}
	if table.FrontType != frontVue3Element {
		return biz(400, "当前尚不支持该前端类型的代码生成")
	}
	return nil
}

func (s *Service) Download(ctx context.Context, tableID int64) ([]byte, error) {
	files, err := s.Preview(ctx, tableID)
	if err != nil {
		return nil, err
	}
	return zipFiles(files)
}

func (s *Service) ready(ctx context.Context, tableID int64) (*Table, []Column, error) {
	table, err := s.tableByID(ctx, tableID)
	if err != nil {
		return nil, nil, err
	}
	if table == nil {
		return nil, nil, biz(codeTableNotExists, "表定义不存在")
	}
	columns, err := s.columnsByTable(ctx, tableID)
	if err != nil {
		return nil, nil, err
	}
	if len(columns) == 0 {
		return nil, nil, biz(codeColumnNotExists, "字段义不存在")
	}
	if isMasterTemplate(table.TemplateType) {
		subs, err := s.subTables(ctx, tableID)
		if err != nil {
			return nil, nil, err
		}
		if len(subs) == 0 {
			return nil, nil, biz(codeMasterNoSub, "主表生成代码失败，原因：它没有子表")
		}
		for _, sub := range subs {
			subColumns, err := s.columnsByTable(ctx, sub.ID)
			if err != nil {
				return nil, nil, err
			}
			found := false
			if sub.SubJoinColumnID != nil {
				for _, column := range subColumns {
					if column.ID == *sub.SubJoinColumnID {
						found = true
						break
					}
				}
			}
			if !found {
				return nil, nil, subColumnMissingErr(sub.ID)
			}
		}
	}
	return table, columns, nil
}

func (s *Service) schema(ctx context.Context, dataSourceID int64, name string) (*DBSchema, error) {
	db, closeDB, err := s.catalog(ctx, dataSourceID)
	if err != nil {
		return nil, err
	}
	defer closeDB()
	return loadSchema(ctx, db, name)
}

func (s *Service) catalog(ctx context.Context, id int64) (*sql.DB, func(), error) {
	if id == 0 {
		if s.DB == nil {
			return nil, nil, biz(500, "数据源(0) 不存在！")
		}
		return s.DB, func() {}, nil
	}
	if s.Sources == nil {
		return nil, nil, biz(500, fmt.Sprintf("数据源(%d) 不存在！", id))
	}
	row, err := s.Sources.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, nil, biz(500, fmt.Sprintf("数据源(%d) 不存在！", id))
	}
	password, err := datasource.DecryptBase64(s.Key, row.Password)
	if err != nil {
		return nil, nil, biz(500, "系统异常")
	}
	db, err := datasource.OpenMySQL(row.URL, row.Username, password)
	if err != nil {
		return nil, nil, biz(codeSourceDown, "数据源配置不正确，无法进行连接")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, nil, biz(codeSourceDown, "数据源配置不正确，无法进行连接")
	}
	return db, func() { _ = db.Close() }, nil
}

func validateSchema(schema *DBSchema) error {
	if schema == nil {
		return biz(codeImportTableNull, "导入的表不存在")
	}
	if schema.Comment == "" {
		return biz(codeTableCommentEmpty, "数据库的表注释未填写")
	}
	if len(schema.Columns) == 0 {
		return biz(codeImportColumnsNull, "导入的字段不存在")
	}
	for _, column := range schema.Columns {
		if column.Comment == "" {
			return columnCommentErr(column.Name)
		}
	}
	return nil
}

// syncPlan 复现 Java 的同步差集。序号按 0 开始的列下标比较，所以导入时写成 1 的序号会被当成变化。
func syncPlan(saved []Column, live []DBColumn) (insert []DBColumn, deleteIDs []int64, none bool) {
	byName := map[string]Column{}
	for _, column := range saved {
		byName[column.ColumnName] = column
	}
	modified := map[string]bool{}
	for index, field := range live {
		column, ok := byName[field.Name]
		if !ok {
			continue
		}
		jdbc, _ := mysqlJavaType(field.ColumnType)
		same := jdbc == column.DataType && field.Nullable == column.Nullable && field.PrimaryKey == column.PrimaryKey &&
			field.Comment == column.ColumnComment && column.OrdinalPosition == index
		if !same {
			modified[field.Name] = true
		}
	}
	liveNames := map[string]bool{}
	for _, field := range live {
		liveNames[field.Name] = true
	}
	for _, column := range saved {
		if !liveNames[column.ColumnName] || modified[column.ColumnName] {
			deleteIDs = append(deleteIDs, column.ID)
		}
	}
	for _, field := range live {
		if _, ok := byName[field.Name]; !ok || modified[field.Name] {
			insert = append(insert, field)
		}
	}
	return insert, deleteIDs, len(insert) == 0 && len(deleteIDs) == 0
}
