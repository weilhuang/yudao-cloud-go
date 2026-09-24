package codegen

import "fmt"

const (
	sceneAdmin = 1

	templateOne          = 1
	templateTree         = 2
	templateMasterNormal = 10
	templateMasterERP    = 11
	templateMasterInner  = 12
	templateSub          = 15

	frontVue3Element = 20

	codeImportTableNull    = 1_001_004_001
	codeTableExists        = 1_001_004_002
	codeImportColumnsNull  = 1_001_004_002
	codeTableNotExists     = 1_001_004_004
	codeColumnNotExists    = 1_001_004_005
	codeSyncNone           = 1_001_004_007
	codeTableCommentEmpty  = 1_001_004_008
	codeColumnCommentEmpty = 1_001_004_009
	codeMasterMissing      = 1_001_004_010
	codeSubColumnMissing   = 1_001_004_011
	codeMasterNoSub        = 1_001_004_012
	codeSourceDown         = 1_001_007_001
)

// Error 是代码生成接口的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func biz(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

func columnCommentErr(name string) *Error {
	return biz(codeColumnCommentEmpty, fmt.Sprintf("数据库的表字段(%s)注释未填写", name))
}

func masterMissingErr(id int64) *Error {
	return biz(codeMasterMissing, fmt.Sprintf("主表(id=%d)定义不存在，请检查", id))
}

func subColumnMissingErr(id int64) *Error {
	return biz(codeSubColumnMissing, fmt.Sprintf("子表的字段(id=%d)不存在，请检查", id))
}

func isMasterTemplate(templateType int) bool {
	return templateType == templateMasterNormal || templateType == templateMasterERP || templateType == templateMasterInner
}

// Table 是 infra_codegen_table 的一行。可选编号用指针，nil 表示库里的 NULL。
type Table struct {
	ID                 int64
	DataSourceConfigID int64
	Scene              int
	TableName          string
	TableComment       string
	Remark             string
	ModuleName         string
	BusinessName       string
	ClassName          string
	ClassComment       string
	Author             string
	TemplateType       int
	FrontType          int
	ParentMenuID       *int64
	MasterTableID      *int64
	SubJoinColumnID    *int64
	SubJoinMany        *bool
	TreeParentColumnID *int64
	TreeNameColumnID   *int64
	CreateTime         int64
	UpdateTime         int64
}

// Column 是 infra_codegen_column 的一行。
type Column struct {
	ID                     int64
	TableID                int64
	ColumnName             string
	DataType               string
	ColumnComment          string
	Nullable               bool
	PrimaryKey             bool
	OrdinalPosition        int
	JavaType               string
	JavaField              string
	DictType               string
	Example                string
	CreateOperation        bool
	UpdateOperation        bool
	ListOperation          bool
	ListOperationCondition string
	ListOperationResult    bool
	HTMLType               string
	CreateTime             int64
}

// PageQuery 是表定义分页条件。CreateFrom/CreateTo 为空表示不限制。
type PageQuery struct {
	PageNo       int
	PageSize     int
	TableName    string
	TableComment string
	ClassName    string
	CreateFrom   string
	CreateTo     string
}

// Page 是分页结果。List 为空时仍是空切片，避免 JSON null。
type Page struct {
	List  []Table
	Total int64
}

// DBTable 是还没导入代码生成的物理表。
type DBTable struct {
	Name    string
	Comment string
}

// DBColumn 是 SHOW FULL COLUMNS 读到的一列，尚未套用界面默认值。
type DBColumn struct {
	Name       string
	ColumnType string
	Comment    string
	Nullable   bool
	PrimaryKey bool
}

// DBSchema 是一张物理表和它的列。
type DBSchema struct {
	Name    string
	Comment string
	Columns []DBColumn
}

// PreviewFile 是预览接口里的一个文件。
type PreviewFile struct {
	FilePath string
	Code     string
}
