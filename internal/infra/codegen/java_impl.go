package codegen

import (
	"fmt"
	"strings"
)

const serviceExceptionClass = "cn.iocoder.yudao.framework.common.exception.util.ServiceExceptionUtil"
const collectionUtilsClass = "cn.iocoder.yudao.framework.common.util.collection.CollectionUtils"

func serviceImplPath(table Table) string {
	return moduleRoot(table, "server") + "/src/main/java/" + dotPath(basePackage) +
		"/module/" + table.ModuleName + "/service/" + table.BusinessName + "/" + table.ClassName + "ServiceImpl.java"
}

func serviceImplJava(table Table, columns []Column) string {
	_, pkg, prefix := sceneView(table.Scene)
	simple := simpleClassName(table)
	classVar := lowerFirst(simple)
	primary := primaryJavaType(columns)
	code := upperCode(simple)
	var b fmtBuilder
	b.printf("package %s.module.%s.service.%s;\n\n", basePackage, table.ModuleName, table.BusinessName)
	b.write("import cn.hutool.core.collection.CollUtil;\nimport org.springframework.stereotype.Service;\nimport jakarta.annotation.Resource;\nimport org.springframework.validation.annotation.Validated;\nimport org.springframework.transaction.annotation.Transactional;\n\nimport java.util.*;\n")
	b.printf("import %s.module.%s.controller.%s.%s.vo.*;\n", basePackage, table.ModuleName, pkg, table.BusinessName)
	b.printf("import %s.module.%s.dal.dataobject.%s.%sDO;\n", basePackage, table.ModuleName, table.BusinessName, table.ClassName)
	b.printf("import %s;\nimport %s;\nimport %s;\n\n", pageResultClass, pageParamClass, beanUtilsClass)
	b.printf("import %s.module.%s.dal.mysql.%s.%sMapper;\n\n", basePackage, table.ModuleName, table.BusinessName, table.ClassName)
	b.printf("import static %s.exception;\nimport static %s.convertList;\nimport static %s.diffList;\nimport static %s.module.%s.enums.ErrorCodeConstants.*;\n\n", serviceExceptionClass, collectionUtilsClass, collectionUtilsClass, basePackage, table.ModuleName)
	b.printf("/**\n * %s Service 实现类\n *\n * @author %s\n */\n@Service\n@Validated\npublic class %sServiceImpl implements %sService {\n\n", table.ClassComment, table.Author, table.ClassName, table.ClassName)
	b.printf("    @Resource\n    private %sMapper %sMapper;\n\n", table.ClassName, classVar)
	b.printf("    @Override\n    public %s create%s(%s%sSaveReqVO createReqVO) {\n", primary, simple, prefix, table.ClassName)
	if table.TemplateType == templateTree {
		b.write(treeCreateChecks(table, columns, simple, "createReqVO"))
	}
	b.printf("        // 插入\n        %sDO %s = BeanUtils.toBean(createReqVO, %sDO.class);\n        %sMapper.insert(%s);\n        // 返回\n        return %s.getId();\n    }\n\n", table.ClassName, classVar, table.ClassName, classVar, classVar, classVar)
	b.printf("    @Override\n    public void update%s(%s%sSaveReqVO updateReqVO) {\n        // 校验存在\n        validate%sExists(updateReqVO.getId());\n", simple, prefix, table.ClassName, simple)
	if table.TemplateType == templateTree {
		b.write(treeUpdateChecks(table, columns, simple))
	}
	b.printf("        // 更新\n        %sDO updateObj = BeanUtils.toBean(updateReqVO, %sDO.class);\n        %sMapper.updateById(updateObj);\n    }\n\n", table.ClassName, table.ClassName, classVar)
	b.printf("    @Override\n    public void delete%s(%s id) {\n        // 校验存在\n        validate%sExists(id);\n", simple, primary, simple)
	if table.TemplateType == templateTree {
		if parent := treeColumn(table, columns, true); parent != nil {
			b.printf("        // 校验是否有子%s\n        if (%sMapper.selectCountBy%s(id) > 0) {\n            throw exception(%s_EXITS_CHILDREN);\n        }\n", table.ClassComment, classVar, javaUpper(parent.JavaField), code)
		}
	}
	b.printf("        // 删除\n        %sMapper.deleteById(id);\n    }\n\n", classVar)
	if table.TemplateType != templateTree {
		b.printf("    @Override\n    public void delete%sListByIds(List<%s> ids) {\n        // 删除\n        %sMapper.deleteByIds(ids);\n    }\n\n", simple, primary, classVar)
	}
	b.printf("    private void validate%sExists(%s id) {\n        if (%sMapper.selectById(id) == null) {\n            throw exception(%s_NOT_EXISTS);\n        }\n    }\n\n", simple, primary, classVar, code)
	if table.TemplateType == templateTree {
		b.write(treeValidators(table, columns, simple, classVar, code))
	}
	b.printf("    @Override\n    public %sDO get%s(%s id) {\n        return %sMapper.selectById(id);\n    }\n\n", table.ClassName, simple, primary, classVar)
	if table.TemplateType != templateTree {
		b.printf("    @Override\n    public PageResult<%sDO> get%sPage(%s%sPageReqVO pageReqVO) {\n        return %sMapper.selectPage(pageReqVO);\n    }\n}\n", table.ClassName, simple, prefix, table.ClassName, classVar)
	} else {
		b.printf("    @Override\n    public List<%sDO> get%sList(%s%sListReqVO listReqVO) {\n        return %sMapper.selectList(listReqVO);\n    }\n}\n", table.ClassName, simple, prefix, table.ClassName, classVar)
	}
	return b.String()
}

func treeCreateChecks(table Table, columns []Column, simple, vo string) string {
	parent, name := treeColumn(table, columns, true), treeColumn(table, columns, false)
	if parent == nil || name == nil {
		return ""
	}
	return fmt.Sprintf("        // 校验%s的有效性\n        validateParent%s(null, %s.get%s());\n        // 校验%s的唯一性\n        validate%s%sUnique(null, %s.get%s(), %s.get%s());\n\n",
		parent.ColumnComment, simple, vo, javaUpper(parent.JavaField),
		name.ColumnComment, simple, javaUpper(name.JavaField), vo, javaUpper(parent.JavaField), vo, javaUpper(name.JavaField))
}

func treeUpdateChecks(table Table, columns []Column, simple string) string {
	parent, name := treeColumn(table, columns, true), treeColumn(table, columns, false)
	if parent == nil || name == nil {
		return ""
	}
	return fmt.Sprintf("        // 校验%s的有效性\n        validateParent%s(updateReqVO.getId(), updateReqVO.get%s());\n        // 校验%s的唯一性\n        validate%s%sUnique(updateReqVO.getId(), updateReqVO.get%s(), updateReqVO.get%s());\n",
		parent.ColumnComment, simple, javaUpper(parent.JavaField),
		name.ColumnComment, simple, javaUpper(name.JavaField), javaUpper(parent.JavaField), javaUpper(name.JavaField))
}

func treeValidators(table Table, columns []Column, simple, classVar, code string) string {
	parent, name := treeColumn(table, columns, true), treeColumn(table, columns, false)
	if parent == nil || name == nil {
		return ""
	}
	parentGet := javaUpper(parent.JavaField)
	nameGet := javaUpper(name.JavaField)
	root := upperCode(parent.JavaField)
	nameCode := upperCode(name.JavaField)
	return fmt.Sprintf(`    private void validateParent%s(Long id, Long %s) {
        if (%s == null || %sDO.%s_ROOT.equals(%s)) {
            return;
        }
        // 1. 不能设置自己为父%s
        if (Objects.equals(id, %s)) {
            throw exception(%s_PARENT_ERROR);
        }
        // 2. 父%s不存在
        %sDO parent%s = %sMapper.selectById(%s);
        if (parent%s == null) {
            throw exception(%s_PARENT_NOT_EXITS);
        }
        // 3. 递归校验父%s，如果父%s是自己的子%s，则报错，避免形成环路
        if (id == null) { // id 为空，说明新增，不需要考虑环路
            return;
        }
        for (int i = 0; i < Short.MAX_VALUE; i++) {
            // 3.1 校验环路
            %s = parent%s.get%s();
            if (Objects.equals(id, %s)) {
                throw exception(%s_PARENT_IS_CHILD);
            }
            // 3.2 继续递归下一级父%s
            if (%s == null || %sDO.%s_ROOT.equals(%s)) {
                break;
            }
            parent%s = %sMapper.selectById(%s);
            if (parent%s == null) {
                break;
            }
        }
    }

    private void validate%s%sUnique(Long id, Long %s, String %s) {
        %sDO %s = %sMapper.selectBy%sAnd%s(%s, %s);
        if (%s == null) {
            return;
        }
        // 如果 id 为空，说明不用比较是否为相同 id 的%s
        if (id == null) {
            throw exception(%s_%s_DUPLICATE);
        }
        if (!Objects.equals(%s.getId(), id)) {
            throw exception(%s_%s_DUPLICATE);
        }
    }

`, simple, parent.JavaField,
		parent.JavaField, simple, root, parent.JavaField,
		table.ClassComment, parent.JavaField, code,
		table.ClassComment, simple, simple, classVar, parent.JavaField, simple, code,
		table.ClassComment, table.ClassComment, table.ClassComment,
		parent.JavaField, simple, parentGet, parent.JavaField, code,
		table.ClassComment, parent.JavaField, simple, root, parent.JavaField,
		simple, classVar, parent.JavaField, simple,
		simple, nameGet, parent.JavaField, name.JavaField,
		simple, classVar, classVar, parentGet, nameGet, parent.JavaField, name.JavaField, classVar,
		table.ClassComment, code, nameCode, classVar, code, nameCode)
}

func treeColumn(table Table, columns []Column, parent bool) *Column {
	id := table.TreeNameColumnID
	if parent {
		id = table.TreeParentColumnID
	}
	if id == nil {
		return nil
	}
	for i := range columns {
		if columns[i].ID == *id {
			return &columns[i]
		}
	}
	return nil
}

func upperCode(name string) string {
	return strings.ToUpper(symbolCase(name, '_'))
}
