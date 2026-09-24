package codegen

import "testing"

func TestBuildTableNames(t *testing.T) {
	table := buildTable("system_user_role", `用户"角色"表`, "芋道", 0)
	if table.ModuleName != "system" || table.BusinessName != "userrole" || table.ClassName != "UserRole" {
		t.Fatalf("%+v", table)
	}
	if table.ClassComment != "用户“角色“" || table.Scene != sceneAdmin || table.TemplateType != templateOne || table.FrontType != frontVue3Element {
		t.Fatalf("默认值不对: %+v", table)
	}
	plain := buildTable("dept", "部门", "a", 0)
	if plain.ModuleName != "dept" || plain.BusinessName != "" || plain.ClassName != "" {
		t.Fatalf("没有下划线时应只保留模块名: %+v", plain)
	}
}

func TestBuildColumnDefaults(t *testing.T) {
	columns := buildColumns(1, []DBColumn{
		{Name: "id", ColumnType: "bigint", Comment: "编号", PrimaryKey: true},
		{Name: "name", ColumnType: "varchar(30)", Comment: "名字"},
		{Name: "status", ColumnType: "tinyint", Comment: "状态"},
		{Name: "flag", ColumnType: "tinyint(1)", Comment: "开关"},
		{Name: "create_time", ColumnType: "datetime", Comment: "创建时间"},
		{Name: "wide", ColumnType: "bit(8)", Comment: "位"},
	}, false)
	if columns[0].JavaType != "Long" || columns[0].CreateOperation || !columns[0].UpdateOperation || columns[0].ListOperation {
		t.Fatalf("主键操作标记不对: %+v", columns[0])
	}
	if columns[1].JavaField != "name" || columns[1].ListOperationCondition != "LIKE" || columns[1].HTMLType != "input" || columns[1].Example != "张三" {
		t.Fatalf("名字列不对: %+v", columns[1])
	}
	if columns[2].JavaType != "Integer" || columns[2].DataType != "TINYINT" || columns[2].HTMLType != "radio" || columns[2].Example != "1" {
		t.Fatalf("状态列不对: %+v", columns[2])
	}
	if columns[3].JavaType != "Boolean" || columns[3].HTMLType != "radio" {
		t.Fatalf("tinyint(1) 应为 Boolean: %+v", columns[3])
	}
	if columns[4].JavaField != "createTime" || columns[4].JavaType != "LocalDateTime" || !columns[4].ListOperation || columns[4].ListOperationCondition != "BETWEEN" || !columns[4].ListOperationResult {
		t.Fatalf("创建时间应参与列表: %+v", columns[4])
	}
	if columns[5].JavaType != "Integer" {
		t.Fatalf("bit 宽字段应把 Byte 收成 Integer: %+v", columns[5])
	}
}

func TestSyncPlanMatchesJavaOrdinal(t *testing.T) {
	saved := []Column{{ID: 9, ColumnName: "id", DataType: "BIGINT", Nullable: false, PrimaryKey: true, ColumnComment: "编号", OrdinalPosition: 1}}
	live := []DBColumn{{Name: "id", ColumnType: "bigint", Comment: "编号", PrimaryKey: true}}
	inserts, deletes, none := syncPlan(saved, live)
	if none || len(deletes) != 1 || deletes[0] != 9 || len(inserts) != 1 {
		t.Fatalf("1 开始的序号和 Java 的 0 下标对不上，应重写列: insert=%+v delete=%v none=%v", inserts, deletes, none)
	}
	saved[0].OrdinalPosition = 0
	inserts, deletes, none = syncPlan(saved, live)
	if !none || len(inserts) != 0 || len(deletes) != 0 {
		t.Fatalf("序号和类型都一致时应拒绝空同步: insert=%+v delete=%v none=%v", inserts, deletes, none)
	}
}
