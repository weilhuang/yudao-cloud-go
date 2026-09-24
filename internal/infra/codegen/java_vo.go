package codegen

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	pageParamClass    = "cn.iocoder.yudao.framework.common.pojo.PageParam"
	pageResultClass   = "cn.iocoder.yudao.framework.common.pojo.PageResult"
	queryWrapperClass = "cn.iocoder.yudao.framework.mybatis.core.query.LambdaQueryWrapperX"
	baseMapperClass   = "cn.iocoder.yudao.framework.mybatis.core.mapper.BaseMapperX"
	dateUtilsClass    = "cn.iocoder.yudao.framework.common.util.date.DateUtils"
	dictFormatClass   = "cn.iocoder.yudao.framework.excel.core.annotations.DictFormat"
	dictConvertClass  = "cn.iocoder.yudao.framework.excel.core.convert.DictConvert"
)

func sceneView(scene int) (name, pkg, prefix string) {
	if scene == 2 {
		return "用户 APP", "app", "App"
	}
	return "管理后台", "admin", ""
}

func voPath(table Table, suffix string) string {
	_, pkg, prefix := sceneView(table.Scene)
	return moduleRoot(table, "server") + "/src/main/java/" + strings.ReplaceAll(basePackage, ".", "/") +
		"/module/" + table.ModuleName + "/controller/" + pkg + "/" + table.BusinessName +
		"/vo/" + prefix + table.ClassName + suffix + ".java"
}

func pageReqPath(table Table) string { return voPath(table, "PageReqVO") }
func listReqPath(table Table) string { return voPath(table, "ListReqVO") }
func respVOPath(table Table) string  { return voPath(table, "RespVO") }
func saveReqPath(table Table) string { return voPath(table, "SaveReqVO") }

func mapperJavaPath(table Table) string {
	return moduleRoot(table, "server") + "/src/main/java/" + strings.ReplaceAll(basePackage, ".", "/") +
		"/module/" + table.ModuleName + "/dal/mysql/" + table.BusinessName + "/" + table.ClassName + "Mapper.java"
}

func pageReqVO(table Table, columns []Column) string {
	return queryVO(table, columns, "分页 Request VO", "PageReqVO", true, true)
}

func listReqVO(table Table, columns []Column) string {
	return queryVO(table, columns, "列表 Request VO", "ListReqVO", false, false)
}

func queryVO(table Table, columns []Column, title, classSuffix string, extendPage, timeByCondition bool) string {
	name, pkg, prefix := sceneView(table.Scene)
	var b strings.Builder
	fmt.Fprintf(&b, "package %s.module.%s.controller.%s.%s.vo;\n\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.WriteString("import lombok.*;\nimport java.util.*;\nimport io.swagger.v3.oas.annotations.media.Schema;\n")
	b.WriteString("import " + pageParamClass + ";\n")
	if hasJavaType(columns, "BigDecimal") {
		b.WriteString("import java.math.BigDecimal;\n")
	}
	if queryNeedsTime(columns, timeByCondition) {
		b.WriteString("import org.springframework.format.annotation.DateTimeFormat;\nimport java.time.LocalDateTime;\n\n")
		b.WriteString("import static " + dateUtilsClass + ".FORMAT_YEAR_MONTH_DAY_HOUR_MINUTE_SECOND;\n")
	}
	fmt.Fprintf(&b, "\n@Schema(description = \"%s - %s%s\")\n@Data\npublic class %s%s%s", name, table.ClassComment, title, prefix, table.ClassName, classSuffix)
	if extendPage {
		b.WriteString(" extends PageParam")
	}
	b.WriteString(" {\n\n")
	for _, column := range columns {
		if !column.ListOperation {
			continue
		}
		if column.ListOperationCondition == "BETWEEN" {
			fmt.Fprintf(&b, "    @Schema(description = \"%s\"%s)\n", column.ColumnComment, exampleAttr(column))
			b.WriteString("    @DateTimeFormat(pattern = FORMAT_YEAR_MONTH_DAY_HOUR_MINUTE_SECOND)\n")
			fmt.Fprintf(&b, "    private %s[] %s;\n\n", column.JavaType, column.JavaField)
			continue
		}
		fmt.Fprintf(&b, "    @Schema(description = \"%s\"%s)\n", column.ColumnComment, exampleAttr(column))
		fmt.Fprintf(&b, "    private %s %s;\n\n", column.JavaType, column.JavaField)
	}
	b.WriteString("}\n")
	return b.String()
}

func respVO(table Table, columns []Column) string {
	name, pkg, prefix := sceneView(table.Scene)
	var b strings.Builder
	fmt.Fprintf(&b, "package %s.module.%s.controller.%s.%s.vo;\n\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.WriteString("import io.swagger.v3.oas.annotations.media.Schema;\nimport lombok.*;\nimport java.util.*;\n")
	if hasJavaType(columns, "BigDecimal") {
		b.WriteString("import java.math.BigDecimal;\n")
	}
	if hasResultTime(columns) {
		b.WriteString("import org.springframework.format.annotation.DateTimeFormat;\nimport java.time.LocalDateTime;\n")
	}
	b.WriteString("import cn.idev.excel.annotation.*;\n")
	if hasDict(columns) {
		b.WriteString("import " + dictFormatClass + ";\nimport " + dictConvertClass + ";\n")
	}
	fmt.Fprintf(&b, "\n@Schema(description = \"%s - %s Response VO\")\n@Data\n@ExcelIgnoreUnannotated\npublic class %s%sRespVO {\n\n", name, table.ClassComment, prefix, table.ClassName)
	for _, column := range columns {
		if !column.ListOperationResult {
			continue
		}
		fmt.Fprintf(&b, "    @Schema(description = \"%s\"%s%s)\n", column.ColumnComment, requiredAttr(column), exampleAttr(column))
		if column.DictType != "" {
			fmt.Fprintf(&b, "    @ExcelProperty(value = \"%s\", converter = DictConvert.class)\n", column.ColumnComment)
			fmt.Fprintf(&b, "    @DictFormat(\"%s\") // TODO 代码优化：建议设置到对应的 DictTypeConstants 枚举类中\n", column.DictType)
		} else {
			fmt.Fprintf(&b, "    @ExcelProperty(\"%s\")\n", column.ColumnComment)
		}
		fmt.Fprintf(&b, "    private %s %s;\n\n", column.JavaType, column.JavaField)
	}
	b.WriteString("}\n")
	return b.String()
}

func saveReqVO(table Table, columns []Column) string {
	name, pkg, prefix := sceneView(table.Scene)
	var b strings.Builder
	fmt.Fprintf(&b, "package %s.module.%s.controller.%s.%s.vo;\n\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.WriteString("import io.swagger.v3.oas.annotations.media.Schema;\nimport lombok.*;\nimport java.util.*;\nimport jakarta.validation.constraints.*;\n")
	if hasJavaType(columns, "BigDecimal") {
		b.WriteString("import java.math.BigDecimal;\n")
	}
	if hasSaveTime(columns) {
		b.WriteString("import org.springframework.format.annotation.DateTimeFormat;\nimport java.time.LocalDateTime;\n")
	}
	fmt.Fprintf(&b, "\n@Schema(description = \"%s - %s新增/修改 Request VO\")\n@Data\npublic class %s%sSaveReqVO {\n\n", name, table.ClassComment, prefix, table.ClassName)
	for _, column := range columns {
		if !column.CreateOperation && !column.UpdateOperation {
			continue
		}
		fmt.Fprintf(&b, "    @Schema(description = \"%s\"%s%s)\n", column.ColumnComment, requiredAttr(column), exampleAttr(column))
		if !column.Nullable && !column.PrimaryKey {
			if column.JavaType == "String" {
				fmt.Fprintf(&b, "    @NotEmpty(message = \"%s不能为空\")\n", column.ColumnComment)
			} else {
				fmt.Fprintf(&b, "    @NotNull(message = \"%s不能为空\")\n", column.ColumnComment)
			}
		}
		fmt.Fprintf(&b, "    private %s %s;\n\n", column.JavaType, column.JavaField)
	}
	b.WriteString("}\n")
	return b.String()
}

func mapperJava(table Table, columns []Column) string {
	_, pkg, prefix := sceneView(table.Scene)
	var b strings.Builder
	fmt.Fprintf(&b, "package %s.module.%s.dal.mysql.%s;\n\n", basePackage, table.ModuleName, table.BusinessName)
	b.WriteString("import java.util.*;\n\n")
	fmt.Fprintf(&b, "import %s;\nimport %s;\nimport %s;\n", pageResultClass, queryWrapperClass, baseMapperClass)
	fmt.Fprintf(&b, "import %s.module.%s.dal.dataobject.%s.%sDO;\n", basePackage, table.ModuleName, table.BusinessName, table.ClassName)
	b.WriteString("import org.apache.ibatis.annotations.Mapper;\n")
	fmt.Fprintf(&b, "import %s.module.%s.controller.%s.%s.vo.*;\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	fmt.Fprintf(&b, "/**\n * %s Mapper\n *\n * @author %s\n */\n@Mapper\npublic interface %sMapper extends BaseMapperX<%sDO> {\n\n", table.ClassComment, table.Author, table.ClassName, table.ClassName)
	vo := prefix + table.ClassName + "PageReqVO"
	method := "selectPage"
	result := "PageResult<" + table.ClassName + "DO>"
	call := "selectPage(reqVO, "
	if table.TemplateType == templateTree {
		vo = prefix + table.ClassName + "ListReqVO"
		method = "selectList"
		result = "List<" + table.ClassName + "DO>"
		call = "selectList("
	}
	fmt.Fprintf(&b, "    default %s %s(%s reqVO) {\n        return %snew LambdaQueryWrapperX<%sDO>()\n", result, method, vo, call, table.ClassName)
	for _, column := range columns {
		if !column.ListOperation {
			continue
		}
		wrapper := condMethod(column.ListOperationCondition)
		if wrapper == "" {
			continue
		}
		getter := javaUpper(column.JavaField)
		fmt.Fprintf(&b, "                .%s(%sDO::get%s, reqVO.get%s())\n", wrapper, table.ClassName, getter, getter)
	}
	fmt.Fprintf(&b, "                .orderByDesc(%sDO::getId));\n\n    }\n}\n", table.ClassName)
	return b.String()
}

func condMethod(condition string) string {
	switch condition {
	case "=":
		return "eqIfPresent"
	case "!=":
		return "neIfPresent"
	case ">":
		return "gtIfPresent"
	case ">=":
		return "geIfPresent"
	case "<":
		return "ltIfPresent"
	case "<=":
		return "leIfPresent"
	case "LIKE":
		return "likeIfPresent"
	case "BETWEEN":
		return "betweenIfPresent"
	default:
		return ""
	}
}

func exampleAttr(column Column) string {
	if column.Example == "" {
		return ""
	}
	return fmt.Sprintf(`, example = "%s"`, column.Example)
}

func requiredAttr(column Column) string {
	if column.Nullable {
		return ""
	}
	return ", requiredMode = Schema.RequiredMode.REQUIRED"
}

func hasJavaType(columns []Column, javaType string) bool {
	for _, column := range columns {
		if column.JavaType == javaType {
			return true
		}
	}
	return false
}

func queryNeedsTime(columns []Column, byCondition bool) bool {
	for _, column := range columns {
		if column.JavaType != "LocalDateTime" {
			continue
		}
		if byCondition && column.ListOperationCondition != "" {
			return true
		}
		if !byCondition && column.ListOperation {
			return true
		}
	}
	return false
}

func hasResultTime(columns []Column) bool {
	for _, column := range columns {
		if column.ListOperationResult && column.JavaType == "LocalDateTime" {
			return true
		}
	}
	return false
}

func hasSaveTime(columns []Column) bool {
	for _, column := range columns {
		if column.JavaType == "LocalDateTime" && (column.CreateOperation || column.UpdateOperation) {
			return true
		}
	}
	return false
}

func hasDict(columns []Column) bool {
	for _, column := range columns {
		if column.DictType != "" {
			return true
		}
	}
	return false
}

func javaUpper(field string) string {
	runes := []rune(field)
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
