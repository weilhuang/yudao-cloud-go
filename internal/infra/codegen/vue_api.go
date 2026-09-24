package codegen

import (
	"strings"
)

func vue3APIPath(table Table) string {
	_, pkg, _ := sceneView(table.Scene)
	return "yudao-ui-" + pkg + "-vue3/src/api/" + table.ModuleName + "/" + table.BusinessName + "/index.ts"
}

func vue3API(table Table, columns []Column) string {
	simple := simpleClassName(table)
	base := "/" + table.ModuleName + "/" + symbolCase(simple, '-')
	var b fmtBuilder
	b.write("import request from '@/config/axios'\nimport type { Dayjs } from 'dayjs';\n\n")
	b.printf("/** %s信息 */\nexport interface %s {\n", table.ClassComment, simple)
	for _, column := range columns {
		if !column.CreateOperation && !column.UpdateOperation {
			continue
		}
		optional := ""
		if column.UpdateOperation && !column.PrimaryKey && !column.Nullable {
			optional = "?"
		}
		b.printf("          %s%s: %s; // %s\n", column.JavaField, optional, tsType(column.JavaType), column.ColumnComment)
	}
	if table.TemplateType == templateTree {
		b.printf("    children?: %s[];\n", simple)
	}
	b.write("}\n\n")
	b.printf("// %s API\nexport const %sApi = {\n", table.ClassComment, simple)
	if table.TemplateType != templateTree {
		b.printf("  // 查询%s分页\n  get%sPage: async (params: any) => {\n    return await request.get({ url: `%s/page`, params })\n  },\n\n", table.ClassComment, simple, base)
	} else {
		b.printf("  // 查询%s列表\n  get%sList: async (params) => {\n    return await request.get({ url: `%s/list`, params })\n  },\n\n", table.ClassComment, simple, base)
	}
	b.printf("  // 查询%s详情\n  get%s: async (id: number) => {\n    return await request.get({ url: `%s/get?id=` + id })\n  },\n\n", table.ClassComment, simple, base)
	b.printf("  // 新增%s\n  create%s: async (data: %s) => {\n    return await request.post({ url: `%s/create`, data })\n  },\n\n", table.ClassComment, simple, simple, base)
	b.printf("  // 修改%s\n  update%s: async (data: %s) => {\n    return await request.put({ url: `%s/update`, data })\n  },\n\n", table.ClassComment, simple, simple, base)
	b.printf("  // 删除%s\n  delete%s: async (id: number) => {\n    return await request.delete({ url: `%s/delete?id=` + id })\n  },\n\n", table.ClassComment, simple, base)
	if table.TemplateType != templateTree {
		b.printf("  /** 批量删除%s */\n  delete%sList: async (ids: number[]) => {\n    return await request.delete({ url: `%s/delete-list?ids=${ids.join(',')}` })\n  },\n\n", table.ClassComment, simple, base)
	}
	b.printf("  // 导出%s Excel\n  export%s: async (params) => {\n    return await request.download({ url: `%s/export-excel`, params })\n  },\n}\n", table.ClassComment, simple, base)
	return b.String()
}

func tsType(javaType string) string {
	switch strings.ToLower(javaType) {
	case "long", "integer", "short", "double", "bigdecimal":
		return "number"
	case "date", "localdate", "localdatetime":
		return "string | Dayjs"
	default:
		return strings.ToLower(javaType)
	}
}
