package codegen

import "strings"

func vue3FormPath(table Table) string {
	_, pkg, _ := sceneView(table.Scene)
	simple := simpleClassName(table)
	return "yudao-ui-" + pkg + "-vue3/src/views/" + table.ModuleName + "/" + table.BusinessName + "/" + simple + "Form.vue"
}

func vue3Form(table Table, columns []Column) string {
	simple := simpleClassName(table)
	classVar := lowerFirst(simple)
	var b fmtBuilder
	b.write("<template>\n  <Dialog :title=\"dialogTitle\" v-model=\"dialogVisible\">\n    <el-form\n      ref=\"formRef\"\n      :model=\"formData\"\n      :rules=\"formRules\"\n      label-width=\"100px\"\n      v-loading=\"formLoading\"\n    >\n")
	for _, column := range columns {
		if !column.CreateOperation && !column.UpdateOperation {
			continue
		}
		b.write(vue3FormItem(table, columns, column))
	}
	b.write("    </el-form>\n    <template #footer>\n      <el-button @click=\"submitForm\" type=\"primary\" :disabled=\"formLoading\">确 定</el-button>\n      <el-button @click=\"dialogVisible = false\">取 消</el-button>\n    </template>\n  </Dialog>\n</template>\n<script setup lang=\"ts\">\n")
	b.write("import { getIntDictOptions, getStrDictOptions, getBoolDictOptions, DICT_TYPE } from '@/utils/dict'\n")
	b.printf("import { %sApi, %s } from '@/api/%s/%s'\n", simple, simple, table.ModuleName, table.BusinessName)
	if table.TemplateType == templateTree {
		b.write("import { defaultProps, handleTree } from '@/utils/tree'\n")
	}
	b.printf("\n/** %s 表单 */\ndefineOptions({ name: '%sForm' })\n\n", table.ClassComment, simple)
	b.write("const { t } = useI18n() // 国际化\nconst message = useMessage() // 消息弹窗\n\n")
	b.write("const dialogVisible = ref(false) // 弹窗的是否展示\nconst dialogTitle = ref('') // 弹窗的标题\nconst formLoading = ref(false) // 表单的加载中：1）修改时的数据加载；2）提交的按钮禁用\nconst formType = ref('') // 表单的类型：create - 新增；update - 修改\nconst formData = ref({\n")
	b.write(vue3FormDefaults(columns))
	b.write("})\nconst formRules = reactive({\n")
	b.write(vue3FormRules(columns))
	b.write("})\nconst formRef = ref() // 表单 Ref\n")
	if table.TemplateType == templateTree {
		b.printf("const %sTree = ref() // 树形结构\n", classVar)
	}
	b.printf(`
/** 打开弹窗 */
const open = async (type: string, id?: number) => {
  dialogVisible.value = true
  dialogTitle.value = t('action.' + type)
  formType.value = type
  resetForm()
  // 修改时，设置数据
  if (id) {
    formLoading.value = true
    try {
      formData.value = await %sApi.get%s(id)
    } finally {
      formLoading.value = false
    }
  }
`, simple, simple)
	if table.TemplateType == templateTree {
		b.printf("  await get%sTree()\n", simple)
	}
	b.write("}\ndefineExpose({ open }) // 提供 open 方法，用于打开弹窗\n\n")
	b.printf(`/** 提交表单 */
const emit = defineEmits(['success']) // 定义 success 事件，用于操作成功后的回调
const submitForm = async () => {
  // 校验表单
  await formRef.value.validate()
  // 提交请求
  formLoading.value = true
  try {
    const data = formData.value as unknown as %s
    if (formType.value === 'create') {
      await %sApi.create%s(data)
      message.success(t('common.createSuccess'))
    } else {
      await %sApi.update%s(data)
      message.success(t('common.updateSuccess'))
    }
    dialogVisible.value = false
    // 发送操作成功的事件
    emit('success')
  } finally {
    formLoading.value = false
  }
}

/** 重置表单 */
const resetForm = () => {
  formData.value = {
`, simple, simple, simple, simple, simple)
	b.write(vue3FormDefaults(columns))
	b.write("  }\n  formRef.value?.resetFields()\n}\n")
	if table.TemplateType == templateTree {
		parent := treeColumn(table, columns, true)
		parentField := "parentId"
		if parent != nil && parent.JavaField != "" {
			parentField = parent.JavaField
		}
		b.printf(`
/** 获得%s树 */
const get%sTree = async () => {
  %sTree.value = []
  const data = await %sApi.get%sList()
  const root: Tree = { id: 0, name: '顶级%s', children: [] }
  root.children = handleTree(data, 'id', '%s')
  %sTree.value.push(root)
}
`, table.ClassComment, simple, classVar, simple, simple, table.ClassComment, parentField, classVar)
	}
	b.write("</script>\n")
	return b.String()
}

func vue3FormItem(table Table, columns []Column, column Column) string {
	if table.TemplateType == templateTree {
		if parent := treeColumn(table, columns, true); parent != nil && parent.ID == column.ID {
			return vue3TreeItem(table, columns, column)
		}
	}
	if column.PrimaryKey && column.HTMLType == "input" {
		return ""
	}
	switch column.HTMLType {
	case "input":
		return vue3Labeled(column, "        <el-input v-model=\"formData."+column.JavaField+"\" placeholder=\"请输入"+column.ColumnComment+"\" />\n")
	case "imageUpload":
		return vue3Labeled(column, "        <UploadImg v-model=\"formData."+column.JavaField+"\" />\n")
	case "fileUpload":
		return vue3Labeled(column, "        <UploadFile v-model=\"formData."+column.JavaField+"\" />\n")
	case "editor":
		return vue3Labeled(column, "        <Editor v-model=\"formData."+column.JavaField+"\" height=\"150px\" />\n")
	case "select":
		return vue3Labeled(column, vue3Options(column, "el-option", false))
	case "checkbox":
		return vue3Labeled(column, vue3Options(column, "el-checkbox", true))
	case "radio":
		return vue3Labeled(column, vue3Radio(column))
	case "datetime":
		return vue3Labeled(column, "        <el-date-picker\n          v-model=\"formData."+column.JavaField+"\"\n          type=\"date\"\n          value-format=\"x\"\n          placeholder=\"选择"+column.ColumnComment+"\"\n        />\n")
	case "textarea":
		return vue3Labeled(column, "        <el-input v-model=\"formData."+column.JavaField+"\" type=\"textarea\" placeholder=\"请输入"+column.ColumnComment+"\" />\n")
	default:
		return ""
	}
}

func vue3TreeItem(table Table, columns []Column, column Column) string {
	name := treeColumn(table, columns, false)
	props := "          :props=\"defaultProps\"\n"
	if name != nil && name.JavaField != "name" {
		props = "          :props=\"{...defaultProps, label: '" + name.JavaField + "'}\"\n"
	}
	classVar := lowerFirst(simpleClassName(table))
	return vue3Labeled(column, "        <el-tree-select\n          v-model=\"formData."+column.JavaField+"\"\n          :data=\""+classVar+"Tree\"\n"+props+"          check-strictly\n          default-expand-all\n          placeholder=\"请选择"+column.ColumnComment+"\"\n        />\n")
}

func vue3Labeled(column Column, inner string) string {
	return "      <el-form-item label=\"" + column.ColumnComment + "\" prop=\"" + column.JavaField + "\">\n" + inner + "      </el-form-item>\n"
}

func vue3Options(column Column, tag string, group bool) string {
	open := "el-select"
	if group {
		open = "el-checkbox-group"
	}
	var b fmtBuilder
	b.printf("        <%s v-model=\"formData.%s\"", open, column.JavaField)
	if !group {
		b.printf(" placeholder=\"请选择%s\"", column.ColumnComment)
	}
	b.write(">\n")
	if column.DictType != "" {
		method := dictMethod(column.JavaType)
		b.printf("          <%s\n            v-for=\"dict in %s(DICT_TYPE.%s)\"\n            :key=\"dict.value\"\n            :label=\"dict.label\"\n            :value=\"dict.value\"\n          />\n", tag, method, strings.ToUpper(column.DictType))
	} else if group {
		b.write("          <el-checkbox label=\"请选择字典生成\" />\n")
	} else {
		b.write("          <el-option label=\"请选择字典生成\" value=\"\" />\n")
	}
	b.printf("        </%s>\n", open)
	return b.String()
}

func vue3Radio(column Column) string {
	var b fmtBuilder
	b.printf("        <el-radio-group v-model=\"formData.%s\">\n", column.JavaField)
	if column.DictType != "" {
		b.printf("          <el-radio\n            v-for=\"dict in %s(DICT_TYPE.%s)\"\n            :key=\"dict.value\"\n            :label=\"dict.value\"\n          >\n            {{ dict.label }}\n          </el-radio>\n", dictMethod(column.JavaType), strings.ToUpper(column.DictType))
	} else {
		b.write("          <el-radio value=\"1\">请选择字典生成</el-radio>\n")
	}
	b.write("        </el-radio-group>\n")
	return b.String()
}

func dictMethod(javaType string) string {
	switch javaType {
	case "Integer", "Long", "Byte", "Short":
		return "getIntDictOptions"
	case "String":
		return "getStrDictOptions"
	case "Boolean":
		return "getBoolDictOptions"
	default:
		return "getDictOptions"
	}
}

func vue3FormDefaults(columns []Column) string {
	var b fmtBuilder
	for _, column := range columns {
		if !column.CreateOperation && !column.UpdateOperation {
			continue
		}
		value := "undefined"
		if column.HTMLType == "checkbox" {
			value = "[]"
		}
		b.printf("  %s: %s,\n", column.JavaField, value)
	}
	return b.String()
}

func vue3FormRules(columns []Column) string {
	var b fmtBuilder
	for _, column := range columns {
		if (!column.CreateOperation && !column.UpdateOperation) || column.Nullable || column.PrimaryKey {
			continue
		}
		trigger := "blur"
		if column.HTMLType == "select" {
			trigger = "change"
		}
		b.printf("  %s: [{ required: true, message: '%s不能为空', trigger: '%s' }],\n", column.JavaField, column.ColumnComment, trigger)
	}
	return b.String()
}
