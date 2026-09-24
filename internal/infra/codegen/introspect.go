package codegen

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 与 Java DatabaseTableServiceImpl 的排除规则一致：未指定表名时跳过工作流、定时任务和带 $ 的系统表。
var skippedTable = regexp.MustCompile(`(?i)^(act_|qrtz_|flw_|impdp_|all_|hs_)`)

func listPhysicalTables(ctx context.Context, db *sql.DB, name, comment string) ([]DBTable, error) {
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME, IFNULL(TABLE_COMMENT,'')
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DBTable
	for rows.Next() {
		var item DBTable
		if err := rows.Scan(&item.Name, &item.Comment); err != nil {
			return nil, err
		}
		if skippedTable.MatchString(item.Name) || strings.Contains(item.Name, "$") {
			continue
		}
		if name != "" && !strings.Contains(item.Name, name) {
			continue
		}
		if comment != "" && !strings.Contains(item.Comment, comment) {
			continue
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	if list == nil {
		list = []DBTable{}
	}
	return list, nil
}

func loadSchema(ctx context.Context, db *sql.DB, name string) (*DBSchema, error) {
	if !safeIdent(name) {
		return nil, nil
	}
	var foundName, foundComment string
	err := db.QueryRowContext(ctx, `SELECT TABLE_NAME, IFNULL(TABLE_COMMENT,'')
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' AND TABLE_NAME = ?`, name).
		Scan(&foundName, &foundComment)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW FULL COLUMNS FROM `%s`", name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	schema := &DBSchema{Name: foundName, Comment: foundComment}
	for rows.Next() {
		values := make([]sql.NullString, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		got := map[string]string{}
		for i, col := range cols {
			got[strings.ToLower(col)] = values[i].String
		}
		schema.Columns = append(schema.Columns, DBColumn{
			Name:       got["field"],
			ColumnType: got["type"],
			Comment:    got["comment"],
			Nullable:   strings.EqualFold(got["null"], "YES"),
			PrimaryKey: strings.EqualFold(got["key"], "PRI"),
		})
	}
	return schema, rows.Err()
}

func safeIdent(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r != '_' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}
