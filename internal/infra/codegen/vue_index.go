package codegen

func vue3IndexPath(table Table) string {
	_, pkg, _ := sceneView(table.Scene)
	return "yudao-ui-" + pkg + "-vue3/src/views/" + table.ModuleName + "/" + table.BusinessName + "/index.vue"
}

func vue3Index(table Table, columns []Column) string {
	simple := simpleClassName(table)
	permission := table.ModuleName + ":" + symbolCase(simple, '-')
	tree := table.TemplateType == templateTree
	var b fmtBuilder
	b.write("<template>\n  <ContentWrap>\n    <!-- 搜索工作栏 -->\n    <el-form\n      class=\"-mb-15px\"\n      :model=\"queryParams\"\n      ref=\"queryFormRef\"\n      :inline=\"true\"\n      label-width=\"68px\"\n    >\n")
	for _, column := range columns {
		if column.ListOperation {
			b.write(vue3SearchItem(column))
		}
	}
	b.printf(`      <el-form-item>
        <el-button @click="handleQuery"><Icon icon="ep:search" class="mr-5px" /> 搜索</el-button>
        <el-button @click="resetQuery"><Icon icon="ep:refresh" class="mr-5px" /> 重置</el-button>
        <el-button
          type="primary"
          plain
          @click="openForm('create')"
          v-hasPermi="['%s:create']"
        >
          <Icon icon="ep:plus" class="mr-5px" /> 新增
        </el-button>
        <el-button
          type="success"
          plain
          @click="handleExport"
          :loading="exportLoading"
          v-hasPermi="['%s:export']"
        >
          <Icon icon="ep:download" class="mr-5px" /> 导出
        </el-button>
`, permission, permission)
	if tree {
		b.write("        <el-button type=\"danger\" plain @click=\"toggleExpandAll\">\n          <Icon icon=\"ep:sort\" class=\"mr-5px\" /> 展开/折叠\n        </el-button>\n")
	} else {
		b.printf("        <el-button\n            type=\"danger\"\n            plain\n            :disabled=\"isEmpty(checkedIds)\"\n            @click=\"handleDeleteBatch\"\n            v-hasPermi=\"['%s:delete']\"\n        >\n          <Icon icon=\"ep:delete\" class=\"mr-5px\" /> 批量删除\n        </el-button>\n", permission)
	}
	b.write("      </el-form-item>\n    </el-form>\n  </ContentWrap>\n\n  <!-- 列表 -->\n  <ContentWrap>\n")
	if tree {
		b.write("    <el-table\n      v-loading=\"loading\"\n      :data=\"list\"\n      :stripe=\"true\"\n      :show-overflow-tooltip=\"true\"\n      row-key=\"id\"\n      :default-expand-all=\"isExpandAll\"\n      v-if=\"refreshTable\"\n    >\n")
	} else {
		b.write("    <el-table\n        row-key=\"id\"\n        v-loading=\"loading\"\n        :data=\"list\"\n        :stripe=\"true\"\n        :show-overflow-tooltip=\"true\"\n        @selection-change=\"handleRowCheckboxChange\"\n    >\n    <el-table-column type=\"selection\" width=\"55\" />\n")
	}
	for _, column := range columns {
		if !column.ListOperationResult {
			continue
		}
		b.write(vue3TableColumn(column))
	}
	b.printf(`      <el-table-column label="操作" align="center" min-width="120px">
        <template #default="scope">
          <el-button
            link
            type="primary"
            @click="openForm('update', scope.row.id)"
            v-hasPermi="['%s:update']"
          >
            编辑
          </el-button>
          <el-button
            link
            type="danger"
            @click="handleDelete(scope.row.id)"
            v-hasPermi="['%s:delete']"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>
`, permission, permission)
	if !tree {
		b.write("    <!-- 分页 -->\n    <Pagination\n      :total=\"total\"\n      v-model:page=\"queryParams.pageNo\"\n      v-model:limit=\"queryParams.pageSize\"\n      @pagination=\"getList\"\n    />\n")
	}
	b.printf("  </ContentWrap>\n\n  <!-- 表单弹窗：添加/修改 -->\n  <%sForm ref=\"formRef\" @success=\"getList\" />\n</template>\n", simple)
	b.write("<script setup lang=\"ts\">\nimport { getIntDictOptions, getStrDictOptions, getBoolDictOptions, DICT_TYPE } from '@/utils/dict'\nimport { isEmpty } from '@/utils/is'\nimport { dateFormatter } from '@/utils/formatTime'\n")
	if tree {
		b.write("import { handleTree } from '@/utils/tree'\n")
	}
	b.write("import download from '@/utils/download'\n")
	b.printf("import { %sApi, %s } from '@/api/%s/%s'\nimport %sForm from './%sForm.vue'\n\n", simple, simple, table.ModuleName, table.BusinessName, simple, simple)
	b.printf("/** %s 列表 */\ndefineOptions({ name: '%s' })\n\n", table.ClassComment, table.ClassName)
	b.write("const message = useMessage() // 消息弹窗\nconst { t } = useI18n() // 国际化\n\nconst loading = ref(true) // 列表的加载中\n")
	b.printf("const list = ref<%s[]>([]) // 列表的数据\n", simple)
	if !tree {
		b.write("const total = ref(0) // 列表的总页数\n")
	}
	b.write("const queryParams = reactive({\n")
	if !tree {
		b.write("  pageNo: 1,\n  pageSize: 10,\n")
	}
	b.write(vue3QueryFields(columns))
	b.write("})\nconst queryFormRef = ref() // 搜索的表单\nconst exportLoading = ref(false) // 导出的加载中\n\n")
	parentField := "parentId"
	if parent := treeColumn(table, columns, true); parent != nil && parent.JavaField != "" {
		parentField = parent.JavaField
	}
	if tree {
		b.printf("const getList = async () => {\n  loading.value = true\n  try {\n    const data = await %sApi.get%sList(queryParams)\n    list.value = handleTree(data, 'id', '%s')\n  } finally {\n    loading.value = false\n  }\n}\n\n", simple, simple, parentField)
	} else {
		b.printf("const getList = async () => {\n  loading.value = true\n  try {\n    const data = await %sApi.get%sPage(queryParams)\n    list.value = data.list\n    total.value = data.total\n  } finally {\n    loading.value = false\n  }\n}\n\n", simple, simple)
	}
	b.write("/** 搜索按钮操作 */\nconst handleQuery = () => {\n")
	if !tree {
		b.write("  queryParams.pageNo = 1\n")
	}
	b.write("  getList()\n}\n\n/** 重置按钮操作 */\nconst resetQuery = () => {\n  queryFormRef.value.resetFields()\n  handleQuery()\n}\n\n/** 添加/修改操作 */\nconst formRef = ref()\nconst openForm = (type: string, id?: number) => {\n  formRef.value.open(type, id)\n}\n\n")
	b.printf("/** 删除按钮操作 */\nconst handleDelete = async (id: number) => {\n  try {\n    // 删除的二次确认\n    await message.delConfirm()\n    // 发起删除\n    await %sApi.delete%s(id)\n    message.success(t('common.delSuccess'))\n    // 刷新列表\n    await getList()\n  } catch {}\n}\n\n", simple, simple)
	if !tree {
		b.printf("/** 批量删除%s */\nconst handleDeleteBatch = async () => {\n  try {\n    // 删除的二次确认\n    await message.delConfirm()\n    await %sApi.delete%sList(checkedIds.value);\n    checkedIds.value = [];\n    message.success(t('common.delSuccess'))\n    await getList();\n  } catch {}\n}\n\nconst checkedIds = ref<number[]>([])\nconst handleRowCheckboxChange = (records: %s[]) => {\n  checkedIds.value = records.map((item) => item.id!);\n}\n\n", table.ClassComment, simple, simple, simple)
	}
	b.printf("/** 导出按钮操作 */\nconst handleExport = async () => {\n  try {\n    // 导出的二次确认\n    await message.exportConfirm()\n    // 发起导出\n    exportLoading.value = true\n    const data = await %sApi.export%s(queryParams)\n    download.excel(data, '%s.xls')\n  } catch {\n  } finally {\n    exportLoading.value = false\n  }\n}\n", simple, simple, table.ClassComment)
	if tree {
		b.write("\n/** 展开/折叠操作 */\nconst isExpandAll = ref(true) // 是否展开，默认全部展开\nconst refreshTable = ref(true) // 重新渲染表格状态\nconst toggleExpandAll = async () => {\n  refreshTable.value = false\n  isExpandAll.value = !isExpandAll.value\n  await nextTick()\n  refreshTable.value = true\n}\n")
	}
	b.write("\n/** 初始化 **/\nonMounted(() => {\n  getList()\n})\n</script>\n")
	return b.String()
}

func vue3SearchItem(column Column) string {
	switch column.HTMLType {
	case "input":
		return "      <el-form-item label=\"" + column.ColumnComment + "\" prop=\"" + column.JavaField + "\">\n        <el-input\n          v-model=\"queryParams." + column.JavaField + "\"\n          placeholder=\"请输入" + column.ColumnComment + "\"\n          clearable\n          @keyup.enter=\"handleQuery\"\n          class=\"!w-240px\"\n        />\n      </el-form-item>\n"
	case "select", "radio":
		inner := "          <el-option label=\"请选择字典生成\" value=\"\" />\n"
		if column.DictType != "" {
			inner = "          <el-option\n            v-for=\"dict in " + dictMethod(column.JavaType) + "(DICT_TYPE." + stringsToUpperOnly(column.DictType) + ")\"\n            :key=\"dict.value\"\n            :label=\"dict.label\"\n            :value=\"dict.value\"\n          />\n"
		}
		return "      <el-form-item label=\"" + column.ColumnComment + "\" prop=\"" + column.JavaField + "\">\n        <el-select\n          v-model=\"queryParams." + column.JavaField + "\"\n          placeholder=\"请选择" + column.ColumnComment + "\"\n          clearable\n          class=\"!w-240px\"\n        >\n" + inner + "        </el-select>\n      </el-form-item>\n"
	case "datetime":
		if column.ListOperationCondition != "BETWEEN" {
			return "      <el-form-item label=\"" + column.ColumnComment + "\" prop=\"" + column.JavaField + "\">\n        <el-date-picker\n          v-model=\"queryParams." + column.JavaField + "\"\n          value-format=\"YYYY-MM-DD\"\n          type=\"date\"\n          placeholder=\"选择" + column.ColumnComment + "\"\n          clearable\n          class=\"!w-240px\"\n        />\n      </el-form-item>\n"
		}
		return "      <el-form-item label=\"" + column.ColumnComment + "\" prop=\"" + column.JavaField + "\">\n        <el-date-picker\n          v-model=\"queryParams." + column.JavaField + "\"\n          value-format=\"YYYY-MM-DD HH:mm:ss\"\n          type=\"daterange\"\n          start-placeholder=\"开始日期\"\n          end-placeholder=\"结束日期\"\n          :default-time=\"[new Date('1 00:00:00'), new Date('1 23:59:59')]\"\n          class=\"!w-220px\"\n        />\n      </el-form-item>\n"
	default:
		return ""
	}
}

func vue3TableColumn(column Column) string {
	if column.JavaType == "LocalDateTime" {
		return "      <el-table-column\n        label=\"" + column.ColumnComment + "\"\n        align=\"center\"\n        prop=\"" + column.JavaField + "\"\n        :formatter=\"dateFormatter\"\n        width=\"180px\"\n      />\n"
	}
	if column.DictType != "" {
		return "      <el-table-column label=\"" + column.ColumnComment + "\" align=\"center\" prop=\"" + column.JavaField + "\">\n        <template #default=\"scope\">\n          <dict-tag :type=\"DICT_TYPE." + stringsToUpperOnly(column.DictType) + "\" :value=\"scope.row." + column.JavaField + "\" />\n        </template>\n      </el-table-column>\n"
	}
	return "      <el-table-column label=\"" + column.ColumnComment + "\" align=\"center\" prop=\"" + column.JavaField + "\" />\n"
}

func vue3QueryFields(columns []Column) string {
	var b fmtBuilder
	for _, column := range columns {
		if !column.ListOperation {
			continue
		}
		if column.ListOperationCondition != "BETWEEN" {
			b.printf("  %s: undefined,\n", column.JavaField)
		}
		if column.HTMLType == "datetime" || column.ListOperationCondition == "BETWEEN" {
			b.printf("  %s: [],\n", column.JavaField)
		}
	}
	return b.String()
}

func stringsToUpperOnly(text string) string {
	out := make([]byte, len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		out[i] = c
	}
	return string(out)
}
