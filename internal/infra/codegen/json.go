package codegen

import "github.com/gin-gonic/gin"

type tableBody struct {
	ID                 *int64  `json:"id"`
	Scene              *int    `json:"scene"`
	TableName          *string `json:"tableName"`
	TableComment       *string `json:"tableComment"`
	Remark             string  `json:"remark"`
	ModuleName         *string `json:"moduleName"`
	BusinessName       *string `json:"businessName"`
	ClassName          *string `json:"className"`
	ClassComment       *string `json:"classComment"`
	Author             *string `json:"author"`
	TemplateType       *int    `json:"templateType"`
	FrontType          *int    `json:"frontType"`
	ParentMenuID       *int64  `json:"parentMenuId"`
	MasterTableID      *int64  `json:"masterTableId"`
	SubJoinColumnID    *int64  `json:"subJoinColumnId"`
	SubJoinMany        *bool   `json:"subJoinMany"`
	TreeParentColumnID *int64  `json:"treeParentColumnId"`
	TreeNameColumnID   *int64  `json:"treeNameColumnId"`
}

func (b tableBody) validate() error {
	switch {
	case b.ID == nil:
		return biz(400, "请求参数不正确")
	case b.Scene == nil:
		return biz(400, "导入类型不能为空")
	case b.TableName == nil:
		return biz(400, "表名称不能为空")
	case b.TableComment == nil:
		return biz(400, "表描述不能为空")
	case b.ModuleName == nil:
		return biz(400, "模块名不能为空")
	case b.BusinessName == nil:
		return biz(400, "业务名不能为空")
	case b.ClassName == nil:
		return biz(400, "类名称不能为空")
	case b.ClassComment == nil:
		return biz(400, "类描述不能为空")
	case b.Author == nil:
		return biz(400, "作者不能为空")
	case b.TemplateType == nil:
		return biz(400, "模板类型不能为空")
	case b.FrontType == nil:
		return biz(400, "前端类型不能为空")
	}
	if *b.Scene == sceneAdmin && b.ParentMenuID == nil {
		return biz(400, "上级菜单不能为空，请前往 [修改生成配置 -> 生成信息] 界面，设置“上级菜单”字段")
	}
	if *b.TemplateType == templateSub && (b.MasterTableID == nil || b.SubJoinColumnID == nil || b.SubJoinMany == nil) {
		return biz(400, "关联的父表信息不全")
	}
	if *b.TemplateType == templateTree && (b.TreeParentColumnID == nil || b.TreeNameColumnID == nil) {
		return biz(400, "关联的树表信息不全")
	}
	return nil
}

func (b tableBody) toTable() Table {
	return Table{
		ID: *b.ID, Scene: *b.Scene, TableName: *b.TableName, TableComment: *b.TableComment, Remark: b.Remark,
		ModuleName: *b.ModuleName, BusinessName: *b.BusinessName, ClassName: *b.ClassName, ClassComment: *b.ClassComment,
		Author: *b.Author, TemplateType: *b.TemplateType, FrontType: *b.FrontType,
		ParentMenuID: b.ParentMenuID, MasterTableID: b.MasterTableID, SubJoinColumnID: b.SubJoinColumnID, SubJoinMany: b.SubJoinMany,
		TreeParentColumnID: b.TreeParentColumnID, TreeNameColumnID: b.TreeNameColumnID,
	}
}

type columnBody struct {
	ID                     *int64  `json:"id"`
	TableID                *int64  `json:"tableId"`
	ColumnName             *string `json:"columnName"`
	DataType               *string `json:"dataType"`
	ColumnComment          *string `json:"columnComment"`
	Nullable               *bool   `json:"nullable"`
	PrimaryKey             *bool   `json:"primaryKey"`
	OrdinalPosition        *int    `json:"ordinalPosition"`
	JavaType               *string `json:"javaType"`
	JavaField              *string `json:"javaField"`
	DictType               string  `json:"dictType"`
	Example                string  `json:"example"`
	CreateOperation        *bool   `json:"createOperation"`
	UpdateOperation        *bool   `json:"updateOperation"`
	ListOperation          *bool   `json:"listOperation"`
	ListOperationCondition *string `json:"listOperationCondition"`
	ListOperationResult    *bool   `json:"listOperationResult"`
	HTMLType               *string `json:"htmlType"`
}

func (b columnBody) toColumn() (Column, error) {
	switch {
	case b.TableID == nil:
		return Column{}, biz(400, "表编号不能为空")
	case b.ColumnName == nil:
		return Column{}, biz(400, "字段名不能为空")
	case b.DataType == nil:
		return Column{}, biz(400, "字段类型不能为空")
	case b.ColumnComment == nil:
		return Column{}, biz(400, "字段描述不能为空")
	case b.Nullable == nil:
		return Column{}, biz(400, "是否允许为空不能为空")
	case b.PrimaryKey == nil:
		return Column{}, biz(400, "是否主键不能为空")
	case b.OrdinalPosition == nil:
		return Column{}, biz(400, "排序不能为空")
	case b.JavaType == nil:
		return Column{}, biz(400, "Java 属性类型不能为空")
	case b.JavaField == nil:
		return Column{}, biz(400, "Java 属性名不能为空")
	case b.CreateOperation == nil:
		return Column{}, biz(400, "是否为 Create 创建操作的字段不能为空")
	case b.UpdateOperation == nil:
		return Column{}, biz(400, "是否为 Update 更新操作的字段不能为空")
	case b.ListOperation == nil:
		return Column{}, biz(400, "是否为 List 查询操作的字段不能为空")
	case b.ListOperationCondition == nil:
		return Column{}, biz(400, "List 查询操作的条件类型不能为空")
	case b.ListOperationResult == nil:
		return Column{}, biz(400, "是否为 List 查询操作的返回字段不能为空")
	case b.HTMLType == nil:
		return Column{}, biz(400, "显示类型不能为空")
	}
	id := int64(0)
	if b.ID != nil {
		id = *b.ID
	}
	return Column{
		ID: id, TableID: *b.TableID, ColumnName: *b.ColumnName, DataType: *b.DataType, ColumnComment: *b.ColumnComment,
		Nullable: *b.Nullable, PrimaryKey: *b.PrimaryKey, OrdinalPosition: *b.OrdinalPosition,
		JavaType: *b.JavaType, JavaField: *b.JavaField, DictType: b.DictType, Example: b.Example,
		CreateOperation: *b.CreateOperation, UpdateOperation: *b.UpdateOperation, ListOperation: *b.ListOperation,
		ListOperationCondition: *b.ListOperationCondition, ListOperationResult: *b.ListOperationResult, HTMLType: *b.HTMLType,
	}, nil
}

func tablesJSON(list []Table) []gin.H {
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, tableJSON(item))
	}
	return out
}

func tableJSON(item Table) gin.H {
	return gin.H{
		"id": item.ID, "scene": item.Scene, "tableName": item.TableName, "tableComment": item.TableComment,
		"remark": item.Remark, "moduleName": item.ModuleName, "businessName": item.BusinessName,
		"className": item.ClassName, "classComment": item.ClassComment, "author": item.Author,
		"templateType": item.TemplateType, "frontType": item.FrontType, "parentMenuId": item.ParentMenuID,
		"masterTableId": item.MasterTableID, "subJoinColumnId": item.SubJoinColumnID, "subJoinMany": item.SubJoinMany,
		"treeParentColumnId": item.TreeParentColumnID, "treeNameColumnId": item.TreeNameColumnID,
		"dataSourceConfigId": item.DataSourceConfigID, "createTime": item.CreateTime, "updateTime": item.UpdateTime,
	}
}

func columnsJSON(list []Column) []gin.H {
	out := make([]gin.H, 0, len(list))
	for _, item := range list {
		out = append(out, gin.H{
			"id": item.ID, "tableId": item.TableID, "columnName": item.ColumnName, "dataType": item.DataType,
			"columnComment": item.ColumnComment, "nullable": item.Nullable, "primaryKey": item.PrimaryKey,
			"ordinalPosition": item.OrdinalPosition, "javaType": item.JavaType, "javaField": item.JavaField,
			"dictType": item.DictType, "example": item.Example, "createOperation": item.CreateOperation,
			"updateOperation": item.UpdateOperation, "listOperation": item.ListOperation,
			"listOperationCondition": item.ListOperationCondition, "listOperationResult": item.ListOperationResult,
			"htmlType": item.HTMLType, "createTime": item.CreateTime,
		})
	}
	return out
}
