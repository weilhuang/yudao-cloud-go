package codegen

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// previewFiles 按冻结配置生成当前已经对齐的文件。
// 默认 front-type=20、vo-type=10、import-enable=false、unit-test-enable=false，因此不含导入类、单元测试和 h2.sql。
// 树表生成 ListReqVO，其它模板生成 PageReqVO。Vue3 Element Plus 还会生成接口、表单和列表页。
func previewFiles(table Table, columns []Column) []PreviewFile {
	queryVO := PreviewFile{FilePath: pageReqPath(table), Code: pageReqVO(table, columns)}
	if table.TemplateType == templateTree {
		queryVO = PreviewFile{FilePath: listReqPath(table), Code: listReqVO(table, columns)}
	}
	files := []PreviewFile{
		{FilePath: "sql/sql.sql", Code: menuSQL(table)},
		queryVO,
		{FilePath: respVOPath(table), Code: respVO(table, columns)},
		{FilePath: saveReqPath(table), Code: saveReqVO(table, columns)},
		{FilePath: javaDOPath(table), Code: javaDO(table, columns)},
		{FilePath: mapperJavaPath(table), Code: mapperJava(table, columns)},
		{FilePath: serviceJavaPath(table), Code: serviceJava(table, columns)},
		{FilePath: serviceImplPath(table), Code: serviceImplJava(table, columns)},
		{FilePath: controllerJavaPath(table), Code: controllerJava(table, columns)},
		{FilePath: mapperXMLPath(table), Code: mapperXML(table)},
		{FilePath: errorCodePath(table), Code: errorCodeJava(table)},
	}
	if table.FrontType == frontVue3Element {
		files = append(files,
			PreviewFile{FilePath: vue3APIPath(table), Code: vue3API(table, columns)},
			PreviewFile{FilePath: vue3FormPath(table), Code: vue3Form(table, columns)},
			PreviewFile{FilePath: vue3IndexPath(table), Code: vue3Index(table, columns)},
		)
	}
	return files
}

const basePackage = "cn.iocoder.yudao"

func moduleRoot(table Table, kind string) string {
	return "yudao-module-" + table.ModuleName + "/yudao-module-" + table.ModuleName + "-" + kind
}

func javaDOPath(table Table) string {
	return moduleRoot(table, "server") + "/src/main/java/" + strings.ReplaceAll(basePackage, ".", "/") +
		"/module/" + table.ModuleName + "/dal/dataobject/" + table.BusinessName + "/" + table.ClassName + "DO.java"
}

func mapperXMLPath(table Table) string {
	return moduleRoot(table, "server") + "/src/main/resources/mapper/" + table.BusinessName + "/" + table.ClassName + "Mapper.xml"
}

func errorCodePath(table Table) string {
	return moduleRoot(table, "api") + "/src/main/java/" + strings.ReplaceAll(basePackage, ".", "/") +
		"/module/" + table.ModuleName + "/enums/ErrorCodeConstants_手动操作.java"
}

func errorCodeJava(table Table) string {
	code := strings.ToUpper(symbolCase(simpleClassName(table), '_'))
	return fmt.Sprintf(`// TODO 待办：请将下面的错误码复制到 yudao-module-%s 模块的 ErrorCodeConstants 类中。注意，请给“TODO 补充编号”设置一个错误码编号！！！
// ========== %s TODO 补充编号 ==========
ErrorCode %s_NOT_EXISTS = new ErrorCode(TODO 补充编号, "%s不存在");
`, table.ModuleName, table.ClassComment, code, table.ClassComment)
}

func mapperXML(table Table) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE mapper PUBLIC "-//mybatis.org//DTD Mapper 3.0//EN" "http://mybatis.org/dtd/mybatis-3-mapper.dtd">
<mapper namespace="%s.module.%s.dal.mysql.%s.%sMapper">

    <!--
        一般情况下，尽可能使用 Mapper 进行 CRUD 增删改查即可。
        无法满足的场景，例如说多表关联查询，才使用 XML 编写 SQL。
        代码生成器暂时只生成 Mapper XML 文件本身，更多推荐 MybatisX 快速开发插件来生成查询。
        文档可见：https://www.iocoder.cn/MyBatis/x-plugins/
     -->

</mapper>
`, basePackage, table.ModuleName, table.BusinessName, table.ClassName)
}

func javaDO(table Table, columns []Column) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s.module.%s.dal.dataobject.%s;\n\n", basePackage, table.ModuleName, table.BusinessName)
	b.WriteString("import lombok.*;\nimport java.util.*;\n")
	seen := map[string]bool{}
	for _, column := range columns {
		imp := ""
		switch column.JavaType {
		case "BigDecimal":
			imp = "import java.math.BigDecimal;\n"
		case "LocalDateTime":
			imp = "import java.time.LocalDateTime;\n"
		}
		if imp != "" && !seen[imp] {
			seen[imp] = true
			b.WriteString(imp)
		}
	}
	b.WriteString("import com.baomidou.mybatisplus.annotation.*;\n")
	b.WriteString("import cn.iocoder.yudao.framework.mybatis.core.dataobject.BaseDO;\n")
	fmt.Fprintf(&b, `
/**
 * %s DO
 *
 * @author %s
 */
@TableName("%s")
@KeySequence("%s_seq") // 用于 Oracle、PostgreSQL、Kingbase、DB2、H2 数据库的主键自增。如果是 MySQL 等数据库，可不写。
@Data
@EqualsAndHashCode(callSuper = true)
@ToString(callSuper = true)
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class %sDO extends BaseDO {

`, table.ClassComment, table.Author, strings.ToLower(table.TableName), strings.ToLower(table.TableName), table.ClassName)
	for _, column := range columns {
		if baseDOField(column.JavaField) {
			continue
		}
		fmt.Fprintf(&b, "    /**\n     * %s\n", column.ColumnComment)
		if column.DictType != "" {
			fmt.Fprintf(&b, "     *\n     * 枚举 {@link TODO %s 对应的类}\n", column.DictType)
		}
		b.WriteString("     */\n")
		if column.PrimaryKey {
			if column.JavaType == "String" {
				b.WriteString("    @TableId(type = IdType.INPUT)\n")
			} else {
				b.WriteString("    @TableId\n")
			}
		}
		fmt.Fprintf(&b, "    private %s %s;\n", column.JavaType, column.JavaField)
	}
	b.WriteString("\n}\n")
	return b.String()
}

func baseDOField(field string) bool {
	switch field {
	case "createTime", "updateTime", "creator", "updater", "deleted", "tenantId":
		return true
	default:
		return false
	}
}

func menuSQL(table Table) string {
	simple := simpleClassName(table)
	strike := symbolCase(simple, '-')
	permission := table.ModuleName + ":" + strike
	names := []string{"查询", "创建", "更新", "删除", "导出"}
	ops := []string{"query", "create", "update", "delete", "export"}
	var b strings.Builder
	b.WriteString("-- 菜单 SQL\n")
	fmt.Fprintf(&b, `INSERT INTO system_menu(
    name, permission, type, sort, parent_id,
    path, icon, component, status, component_name
)
VALUES (
    '%s管理', '', 2, 0, %s,
    '%s', '', '%s/%s/index', 0, '%s'
);

-- 按钮父菜单ID
SELECT @parentId := LAST_INSERT_ID();

-- 按钮 SQL
`, table.ClassComment, parentMenuID(table), strike, table.ModuleName, table.BusinessName, table.ClassName)
	for i, name := range names {
		fmt.Fprintf(&b, `INSERT INTO system_menu(
    name, permission, type, sort, parent_id,
    path, icon, component, status
)
VALUES (
    '%s%s', '%s:%s', 3, %d, @parentId,
    '', '', '', 0
);
`, table.ClassComment, name, permission, ops[i], i+1)
	}
	return b.String()
}

func h2SQL(table Table, columns []Column) string {
	primary := primaryColumn(columns)
	var b strings.Builder
	fmt.Fprintf(&b, "-- 将该建表 SQL 语句，添加到 yudao-module-%s-biz 模块的 test/resources/sql/create_tables.sql 文件里\n", table.ModuleName)
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS \"%s\" (\n", strings.ToLower(table.TableName))
	for _, column := range columns {
		b.WriteString(h2Column(column))
	}
	key := "id"
	if primary != nil {
		key = strings.ToLower(primary.ColumnName)
	}
	fmt.Fprintf(&b, "    PRIMARY KEY (\"%s\")\n", key)
	fmt.Fprintf(&b, ") COMMENT '%s';\n\n", strings.ReplaceAll(table.TableComment, "'", "‘"))
	fmt.Fprintf(&b, "-- 将该删表 SQL 语句，添加到 yudao-module-%s-biz 模块的 test/resources/sql/clean.sql 文件里\n", table.ModuleName)
	fmt.Fprintf(&b, "DELETE FROM \"%s\";\n", table.TableName)
	return b.String()
}

func h2Column(column Column) string {
	if column.PrimaryKey {
		if column.JavaType == "String" {
			return fmt.Sprintf("    \"%s\" varchar NOT NULL,\n", column.JavaField)
		}
		return fmt.Sprintf("    \"%s\" %s NOT NULL GENERATED BY DEFAULT AS IDENTITY,\n", column.JavaField, h2Type(column.JavaType))
	}
	switch column.ColumnName {
	case "create_time":
		return "    \"create_time\" datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n"
	case "update_time":
		return "    \"update_time\" datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,\n"
	case "creator", "updater":
		return fmt.Sprintf("    \"%s\" %s DEFAULT '',\n", column.ColumnName, h2Type(column.JavaType))
	case "deleted":
		return "    \"deleted\" bit NOT NULL DEFAULT FALSE,\n"
	case "tenant_id":
		return "    \"tenant_id\" bigint NOT NULL DEFAULT 0,\n"
	default:
		nullSQL := ""
		if !column.Nullable {
			nullSQL = " NOT NULL"
		}
		return fmt.Sprintf("    \"%s\" %s%s,\n", strings.ToLower(column.ColumnName), h2Type(column.JavaType), nullSQL)
	}
}

func h2Type(javaType string) string {
	switch javaType {
	case "Long":
		return "bigint"
	case "Integer":
		return "int"
	case "Boolean":
		return "bit"
	case "Date":
		return "datetime"
	default:
		return "varchar"
	}
}

func primaryColumn(columns []Column) *Column {
	for i := range columns {
		if columns[i].PrimaryKey {
			return &columns[i]
		}
	}
	return nil
}

func parentMenuID(table Table) string {
	if table.ParentMenuID == nil {
		return "0"
	}
	return strconv.FormatInt(*table.ParentMenuID, 10)
}

func simpleClassName(table Table) string {
	if strings.EqualFold(table.ClassName, table.ModuleName) {
		return table.ClassName
	}
	return strings.TrimPrefix(table.ClassName, upperFirst(table.ModuleName))
}

// symbolCase 对齐 Hutool toSymbolCase：连续大写字母不拆开，小写前的大写才插入符号。
func symbolCase(text string, symbol byte) string {
	runes := []rune(text)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if prev != rune(symbol) && (unicode.IsLower(prev) || (next != 0 && unicode.IsLower(next))) {
					b.WriteByte(symbol)
				}
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func zipFiles(files []PreviewFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, file := range files {
		w, err := zw.Create(file.FilePath)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := w.Write([]byte(file.Code)); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
