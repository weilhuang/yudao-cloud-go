package codegen

import (
	"strings"
	"unicode"
)

// baseDOFields 与 Java BaseDO 加上 tenantId 一致。这些字段默认不出现在新增、修改和列表条件里。
var baseDOFields = map[string]bool{
	"createTime": true,
	"updateTime": true,
	"creator":    true,
	"updater":    true,
	"deleted":    true,
	"tenantId":   true,
}

// htmlBySuffix 按 Java 的插入顺序匹配字段名后缀，先命中的优先。
var htmlBySuffix = []struct{ suffix, html string }{
	{"status", "radio"},
	{"sex", "radio"},
	{"type", "select"},
	{"image", "imageUpload"},
	{"file", "fileUpload"},
	{"content", "editor"},
	{"description", "editor"},
	{"demo", "editor"},
	{"time", "datetime"},
	{"date", "datetime"},
}

// buildTable 从物理表名和注释推出模块、业务名和类名。没有下划线时，业务名和类名为空。
func buildTable(name, comment, author string, dataSourceID int64) Table {
	lower := strings.ToLower(name)
	module, rest, hasUnderscore := strings.Cut(lower, "_")
	if !hasUnderscore {
		rest = ""
	}
	business := strings.ToLower(toCamel(rest))
	className := upperFirst(toCamel(rest))
	return Table{
		DataSourceConfigID: dataSourceID,
		Scene:              sceneAdmin,
		TableName:          name,
		TableComment:       comment,
		ModuleName:         module,
		BusinessName:       business,
		ClassName:          className,
		ClassComment:       strings.TrimSuffix(sanitizeComment(comment), "表"),
		Author:             author,
		TemplateType:       templateOne,
		FrontType:          frontVue3Element,
	}
}

// buildColumns 给导入或同步上来的列补上 Java 类型、界面控件和是否参与增删改查。
// 序号从 1 开始，与 Java CodegenBuilder 一致。同步时 Java 用 0 开始的下标比较，因此刚导入的表再同步会被看成有变化。
func buildColumns(tableID int64, fields []DBColumn, noPrimary bool) []Column {
	out := make([]Column, 0, len(fields))
	for i, field := range fields {
		jdbc, javaType := mysqlJavaType(field.ColumnType)
		if javaType == "Byte" {
			javaType = "Integer"
		}
		column := Column{
			TableID:         tableID,
			ColumnName:      field.Name,
			DataType:        jdbc,
			ColumnComment:   sanitizeComment(field.Comment),
			Nullable:        field.Nullable,
			PrimaryKey:      field.PrimaryKey,
			OrdinalPosition: i + 1,
			JavaType:        javaType,
			JavaField:       lowerFirst(toCamel(strings.ToLower(field.Name))),
		}
		applyOperation(&column)
		applyHTML(&column)
		column.Example = exampleOf(column.JavaField, column.ColumnName)
		out = append(out, column)
	}
	if noPrimary && len(out) > 0 {
		out[0].PrimaryKey = true
		applyOperation(&out[0])
	}
	return out
}

func applyOperation(column *Column) {
	field := column.JavaField
	column.CreateOperation = !excluded(field, true) && !column.PrimaryKey
	column.UpdateOperation = !baseDOFields[field] || column.PrimaryKey
	column.ListOperation = !listExcluded(field) && !column.PrimaryKey
	column.ListOperationCondition = "="
	lower := strings.ToLower(field)
	switch {
	case strings.HasSuffix(lower, "name"):
		column.ListOperationCondition = "LIKE"
	case strings.HasSuffix(lower, "time"), strings.HasSuffix(lower, "date"):
		column.ListOperationCondition = "BETWEEN"
	}
	column.ListOperationResult = field == "createTime" || !baseDOFields[field]
}

func excluded(field string, create bool) bool {
	if field == "id" && create {
		return true
	}
	return baseDOFields[field]
}

func listExcluded(field string) bool {
	if field == "id" {
		return true
	}
	return baseDOFields[field] && field != "createTime"
}

func applyHTML(column *Column) {
	lower := strings.ToLower(column.JavaField)
	for _, item := range htmlBySuffix {
		if strings.HasSuffix(lower, item.suffix) {
			column.HTMLType = item.html
			break
		}
	}
	if column.JavaType == "Boolean" {
		column.HTMLType = "radio"
	}
	if column.JavaType == "LocalDateTime" {
		column.HTMLType = "datetime"
	}
	if column.HTMLType == "" {
		column.HTMLType = "input"
	}
}

// exampleOf 用固定示例代替 Java 的随机值，避免同一张表每次导入预览都不一样。
func exampleOf(javaField, columnName string) string {
	field := strings.ToLower(javaField)
	name := strings.ToLower(columnName)
	switch {
	case hasSuffix(field, "id", "price", "count"):
		return "1"
	case strings.HasSuffix(field, "name"):
		return "张三"
	case hasSuffix(field, "status", "type"):
		return "1"
	case strings.HasSuffix(name, "url"):
		return "https://www.iocoder.cn"
	case strings.HasSuffix(name, "reason"):
		return "不喜欢"
	case hasSuffix(name, "description", "memo", "remark"):
		return "你猜"
	default:
		return ""
	}
}

func hasSuffix(value string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func sanitizeComment(comment string) string {
	comment = strings.ReplaceAll(comment, `"`, "“")
	return strings.ReplaceAll(comment, `'`, "‘")
}

func mysqlJavaType(columnType string) (jdbc, java string) {
	raw := strings.ToLower(strings.TrimSpace(columnType))
	switch {
	case strings.Contains(raw, "point"):
		return "BINARY", "byte[]"
	case strings.Contains(raw, "char"), strings.Contains(raw, "text"), strings.Contains(raw, "json"), strings.Contains(raw, "enum"):
		jdbc = "VARCHAR"
		if strings.Contains(raw, "text") {
			jdbc = "LONGVARCHAR"
		} else if strings.HasPrefix(raw, "char") {
			jdbc = "CHAR"
		}
		return jdbc, "String"
	case strings.Contains(raw, "bigint"):
		return "BIGINT", "Long"
	case strings.Contains(raw, "tinyint(1)"), strings.Contains(raw, "bit(1)"):
		if strings.Contains(raw, "bit") {
			return "BIT", "Boolean"
		}
		return "TINYINT", "Boolean"
	case strings.Contains(raw, "bit"):
		return "BIT", "Byte"
	case strings.Contains(raw, "int"):
		jdbc = "INTEGER"
		if strings.HasPrefix(raw, "tinyint") {
			jdbc = "TINYINT"
		} else if strings.HasPrefix(raw, "smallint") {
			jdbc = "SMALLINT"
		}
		return jdbc, "Integer"
	case strings.Contains(raw, "decimal"):
		return "DECIMAL", "BigDecimal"
	case strings.Contains(raw, "clob"):
		return "CLOB", "Clob"
	case strings.Contains(raw, "blob"):
		return "BLOB", "Blob"
	case strings.Contains(raw, "binary"):
		return "BINARY", "byte[]"
	case strings.Contains(raw, "float"):
		return "FLOAT", "Float"
	case strings.Contains(raw, "double"):
		return "DOUBLE", "Double"
	case strings.Contains(raw, "date"), strings.Contains(raw, "time"), strings.Contains(raw, "year"):
		return dateJavaType(raw)
	default:
		return "VARCHAR", "String"
	}
}

func dateJavaType(raw string) (string, string) {
	base := raw
	if i := strings.IndexByte(base, '('); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "date":
		return "DATE", "LocalDate"
	case "time":
		return "TIME", "LocalTime"
	case "year":
		return "DATE", "Year"
	default:
		return "TIMESTAMP", "LocalDateTime"
	}
}

func toCamel(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.Split(name, "_")
	var b strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			b.WriteString(strings.ToLower(part))
			continue
		}
		b.WriteString(upperFirst(strings.ToLower(part)))
	}
	return b.String()
}

func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	r := []rune(value)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	r := []rune(value)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}
