package codegen

import (
	"fmt"
)

const (
	commonResultClass = "cn.iocoder.yudao.framework.common.pojo.CommonResult"
	beanUtilsClass    = "cn.iocoder.yudao.framework.common.util.object.BeanUtils"
	excelUtilsClass   = "cn.iocoder.yudao.framework.excel.core.util.ExcelUtils"
	apiAccessLogClass = "cn.iocoder.yudao.framework.apilog.core.annotation.ApiAccessLog"
	operateTypeClass  = "cn.iocoder.yudao.framework.apilog.core.enums.OperateTypeEnum"
)

func serviceJavaPath(table Table) string {
	return moduleRoot(table, "server") + "/src/main/java/" + dotPath(basePackage) +
		"/module/" + table.ModuleName + "/service/" + table.BusinessName + "/" + table.ClassName + "Service.java"
}

func controllerJavaPath(table Table) string {
	_, pkg, prefix := sceneView(table.Scene)
	return moduleRoot(table, "server") + "/src/main/java/" + dotPath(basePackage) +
		"/module/" + table.ModuleName + "/controller/" + pkg + "/" + table.BusinessName + "/" + prefix + table.ClassName + "Controller.java"
}

func serviceJava(table Table, columns []Column) string {
	_, pkg, prefix := sceneView(table.Scene)
	simple := simpleClassName(table)
	primary := primaryJavaType(columns)
	var b fmtBuilder
	b.printf("package %s.module.%s.service.%s;\n\n", basePackage, table.ModuleName, table.BusinessName)
	b.write("import java.util.*;\nimport jakarta.validation.*;\n")
	b.printf("import %s.module.%s.controller.%s.%s.vo.*;\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.printf("import %s.module.%s.dal.dataobject.%s.%sDO;\n", basePackage, table.ModuleName, table.BusinessName, table.ClassName)
	b.printf("import %s;\nimport %s;\n\n", pageResultClass, pageParamClass)
	b.printf("/**\n * %s Service 接口\n *\n * @author %s\n */\npublic interface %sService {\n\n", table.ClassComment, table.Author, table.ClassName)
	b.printf("    /**\n     * 创建%s\n     *\n     * @param createReqVO 创建信息\n     * @return 编号\n     */\n", table.ClassComment)
	b.printf("    %s create%s(@Valid %s%sSaveReqVO createReqVO);\n\n", primary, simple, prefix, table.ClassName)
	b.printf("    /**\n     * 更新%s\n     *\n     * @param updateReqVO 更新信息\n     */\n", table.ClassComment)
	b.printf("    void update%s(@Valid %s%sSaveReqVO updateReqVO);\n\n", simple, prefix, table.ClassName)
	b.printf("    /**\n     * 删除%s\n     *\n     * @param id 编号\n     */\n    void delete%s(%s id);\n\n", table.ClassComment, simple, primary)
	if table.TemplateType != templateTree {
		b.printf("    /**\n     * 批量删除%s\n     *\n     * @param ids 编号\n     */\n    void delete%sListByIds(List<%s> ids);\n\n", table.ClassComment, simple, primary)
	}
	b.printf("    /**\n     * 获得%s\n     *\n     * @param id 编号\n     * @return %s\n     */\n    %sDO get%s(%s id);\n\n", table.ClassComment, table.ClassComment, table.ClassName, simple, primary)
	if table.TemplateType != templateTree {
		b.printf("    /**\n     * 获得%s分页\n     *\n     * @param pageReqVO 分页查询\n     * @return %s分页\n     */\n", table.ClassComment, table.ClassComment)
		b.printf("    PageResult<%sDO> get%sPage(%s%sPageReqVO pageReqVO);\n}\n", table.ClassName, simple, prefix, table.ClassName)
	} else {
		b.printf("    /**\n     * 获得%s列表\n     *\n     * @param listReqVO 查询条件\n     * @return %s列表\n     */\n", table.ClassComment, table.ClassComment)
		b.printf("    List<%sDO> get%sList(%s%sListReqVO listReqVO);\n}\n", table.ClassName, simple, prefix, table.ClassName)
	}
	return b.String()
}

func controllerJava(table Table, columns []Column) string {
	name, pkg, prefix := sceneView(table.Scene)
	simple := simpleClassName(table)
	classVar := lowerFirst(simple)
	primary := primaryJavaType(columns)
	permission := table.ModuleName + ":" + symbolCase(simple, '-')
	admin := table.Scene != 2
	var b fmtBuilder
	b.printf("package %s.module.%s.controller.%s.%s;\n\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.write("import org.springframework.web.bind.annotation.*;\nimport jakarta.annotation.Resource;\nimport org.springframework.validation.annotation.Validated;\n")
	if admin {
		b.write("import org.springframework.security.access.prepost.PreAuthorize;\n")
	}
	b.write("\nimport io.swagger.v3.oas.annotations.tags.Tag;\nimport io.swagger.v3.oas.annotations.Parameter;\nimport io.swagger.v3.oas.annotations.Operation;\n\n")
	b.write("import jakarta.validation.constraints.*;\nimport jakarta.validation.*;\nimport jakarta.servlet.http.*;\nimport java.util.*;\nimport java.io.IOException;\n\n")
	b.printf("import %s;\nimport %s;\nimport %s;\nimport %s;\nimport static %s.success;\n\n", pageParamClass, pageResultClass, commonResultClass, beanUtilsClass, commonResultClass)
	b.printf("import %s;\n\nimport %s;\nimport static %s.*;\n\n", excelUtilsClass, apiAccessLogClass, operateTypeClass)
	b.printf("import %s.module.%s.controller.%s.%s.vo.*;\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.printf("import %s.module.%s.dal.dataobject.%s.%sDO;\n", basePackage, table.ModuleName, table.BusinessName, table.ClassName)
	b.printf("import %s.module.%s.service.%s.%sService;\n\n", basePackage, table.ModuleName, table.BusinessName, table.ClassName)
	b.printf("@Tag(name = \"%s - %s\")\n@RestController\n@RequestMapping(\"/%s/%s\")\n@Validated\npublic class %s%sController {\n\n", name, table.ClassComment, table.ModuleName, symbolCase(simple, '-'), prefix, table.ClassName)
	b.printf("    @Resource\n    private %sService %sService;\n\n", table.ClassName, classVar)
	b.write(controllerWrite(table, simple, classVar, primary, prefix, permission, admin, "create", "Post", "创建", fmt.Sprintf("public CommonResult<%s> create%s(@Valid @RequestBody %s%sSaveReqVO createReqVO) {\n        return success(%sService.create%s(createReqVO));\n    }", primary, simple, prefix, table.ClassName, classVar, simple)))
	b.write(controllerWrite(table, simple, classVar, primary, prefix, permission, admin, "update", "Put", "更新", fmt.Sprintf("public CommonResult<Boolean> update%s(@Valid @RequestBody %s%sSaveReqVO updateReqVO) {\n        %sService.update%s(updateReqVO);\n        return success(true);\n    }", simple, prefix, table.ClassName, classVar, simple)))
	b.write(controllerDelete(table, simple, classVar, primary, permission, admin))
	if table.TemplateType != templateTree {
		b.write(controllerBatchDelete(table, simple, classVar, primary, permission, admin))
	}
	b.write(controllerGet(table, simple, classVar, primary, prefix, permission, admin))
	if table.TemplateType != templateTree {
		b.write(controllerPage(table, simple, classVar, prefix, permission, admin))
		b.write(controllerExport(table, simple, classVar, prefix, permission, admin, true))
	} else {
		b.write(controllerList(table, simple, classVar, prefix, permission, admin))
		b.write(controllerExport(table, simple, classVar, prefix, permission, admin, false))
	}
	b.write("}\n")
	return b.String()
}

func controllerWrite(table Table, simple, classVar, primary, prefix, permission string, admin bool, op, verb, summary, body string) string {
	var b fmtBuilder
	b.printf("    @%sMapping(\"/%s\")\n    @Operation(summary = \"%s%s\")\n", verb, op, summary, table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:%s')\")\n", permission, op)
	}
	b.printf("    %s\n\n", body)
	return b.String()
}

func controllerDelete(table Table, simple, classVar, primary, permission string, admin bool) string {
	var b fmtBuilder
	b.printf("    @DeleteMapping(\"/delete\")\n    @Operation(summary = \"删除%s\")\n    @Parameter(name = \"id\", description = \"编号\", required = true)\n", table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:delete')\")\n", permission)
	}
	b.printf("    public CommonResult<Boolean> delete%s(@RequestParam(\"id\") %s id) {\n        %sService.delete%s(id);\n        return success(true);\n    }\n\n", simple, primary, classVar, simple)
	return b.String()
}

func controllerBatchDelete(table Table, simple, classVar, primary, permission string, admin bool) string {
	var b fmtBuilder
	b.printf("    @DeleteMapping(\"/delete-list\")\n    @Parameter(name = \"ids\", description = \"编号\", required = true)\n    @Operation(summary = \"批量删除%s\")\n", table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:delete')\")\n", permission)
	}
	b.printf("    public CommonResult<Boolean> delete%sList(@RequestParam(\"ids\") List<%s> ids) {\n        %sService.delete%sListByIds(ids);\n        return success(true);\n    }\n\n", simple, primary, classVar, simple)
	return b.String()
}

func controllerGet(table Table, simple, classVar, primary, prefix, permission string, admin bool) string {
	var b fmtBuilder
	b.printf("    @GetMapping(\"/get\")\n    @Operation(summary = \"获得%s\")\n    @Parameter(name = \"id\", description = \"编号\", required = true, example = \"1024\")\n", table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:query')\")\n", permission)
	}
	b.printf("    public CommonResult<%s%sRespVO> get%s(@RequestParam(\"id\") %s id) {\n", prefix, table.ClassName, simple, primary)
	b.printf("        %sDO %s = %sService.get%s(id);\n", table.ClassName, classVar, classVar, simple)
	b.printf("        return success(BeanUtils.toBean(%s, %s%sRespVO.class));\n    }\n\n", classVar, prefix, table.ClassName)
	return b.String()
}

func controllerPage(table Table, simple, classVar, prefix, permission string, admin bool) string {
	var b fmtBuilder
	b.printf("    @GetMapping(\"/page\")\n    @Operation(summary = \"获得%s分页\")\n", table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:query')\")\n", permission)
	}
	b.printf("    public CommonResult<PageResult<%s%sRespVO>> get%sPage(@Valid %s%sPageReqVO pageReqVO) {\n", prefix, table.ClassName, simple, prefix, table.ClassName)
	b.printf("        PageResult<%sDO> pageResult = %sService.get%sPage(pageReqVO);\n", table.ClassName, classVar, simple)
	b.printf("        return success(BeanUtils.toBean(pageResult, %s%sRespVO.class));\n    }\n\n", prefix, table.ClassName)
	return b.String()
}

func controllerList(table Table, simple, classVar, prefix, permission string, admin bool) string {
	var b fmtBuilder
	b.printf("    @GetMapping(\"/list\")\n    @Operation(summary = \"获得%s列表\")\n", table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:query')\")\n", permission)
	}
	b.printf("    public CommonResult<List<%s%sRespVO>> get%sList(@Valid %s%sListReqVO listReqVO) {\n", prefix, table.ClassName, simple, prefix, table.ClassName)
	b.printf("        List<%sDO> list = %sService.get%sList(listReqVO);\n", table.ClassName, classVar, simple)
	b.printf("        return success(BeanUtils.toBean(list, %s%sRespVO.class));\n    }\n\n", prefix, table.ClassName)
	return b.String()
}

func controllerExport(table Table, simple, classVar, prefix, permission string, admin, page bool) string {
	var b fmtBuilder
	b.printf("    @GetMapping(\"/export-excel\")\n    @Operation(summary = \"导出%s Excel\")\n", table.ClassComment)
	if admin {
		b.printf("    @PreAuthorize(\"@ss.hasPermission('%s:export')\")\n", permission)
	}
	b.write("    @ApiAccessLog(operateType = EXPORT)\n")
	if page {
		b.printf("    public void export%sExcel(@Valid %s%sPageReqVO pageReqVO,\n              HttpServletResponse response) throws IOException {\n", simple, prefix, table.ClassName)
		b.write("        pageReqVO.setPageSize(PageParam.PAGE_SIZE_NONE);\n")
		b.printf("        List<%sDO> list = %sService.get%sPage(pageReqVO).getList();\n", table.ClassName, classVar, simple)
	} else {
		b.printf("    public void export%sExcel(@Valid %s%sListReqVO listReqVO,\n              HttpServletResponse response) throws IOException {\n", simple, prefix, table.ClassName)
		b.printf("        List<%sDO> list = %sService.get%sList(listReqVO);\n", table.ClassName, classVar, simple)
	}
	b.printf("        // 导出 Excel\n        ExcelUtils.write(response, \"%s.xls\", \"数据\", %s%sRespVO.class,\n                        BeanUtils.toBean(list, %s%sRespVO.class));\n    }\n\n", table.ClassComment, prefix, table.ClassName, prefix, table.ClassName)
	return b.String()
}

func primaryJavaType(columns []Column) string {
	if column := primaryColumn(columns); column != nil && column.JavaType != "" {
		return column.JavaType
	}
	return "Long"
}

func dotPath(pkg string) string {
	out := make([]byte, 0, len(pkg))
	for i := 0; i < len(pkg); i++ {
		if pkg[i] == '.' {
			out = append(out, '/')
			continue
		}
		out = append(out, pkg[i])
	}
	return string(out)
}

type fmtBuilder struct{ b []byte }

func (f *fmtBuilder) write(text string) { f.b = append(f.b, text...) }

func (f *fmtBuilder) printf(format string, args ...any) {
	f.b = append(f.b, fmt.Sprintf(format, args...)...)
}

func (f *fmtBuilder) String() string { return string(f.b) }
