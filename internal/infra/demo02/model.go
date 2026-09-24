package demo02

const (
	parentRoot = int64(0)

	codeNotExists     = 1_001_201_001
	codeHasChildren   = 1_001_201_002
	codeParentMissing = 1_001_201_003
	codeParentSelf    = 1_001_201_004
	codeNameDuplicate = 1_001_201_005
	codeParentIsChild = 1_001_201_006
)

// Error 是示例分类接口的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func biz(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

// Category 是 yudao_demo02_category 的一行。
type Category struct {
	ID         int64
	Name       string
	ParentID   int64
	CreateTime int64
}

// Save 是新增和修改的入参。父级 0 表示根节点。
type Save struct {
	ID       int64
	Name     string
	ParentID int64
}

// Query 是列表和导出共用的筛选。
type Query struct {
	Name       string
	ParentID   *int64
	CreateFrom string
	CreateTo   string
}
