package demo01

// 示例联系人不存在。删除和修改时，本租户查不到就返回这个错误；查询单条则 data 为 null。
const codeNotExists = 1_001_201_000

// Error 是示例联系人接口的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func missing() *Error { return &Error{Code: codeNotExists, Msg: "示例联系人不存在"} }

// Contact 是 yudao_demo01_contact 的一行。时间用毫秒时间戳，和 JSON 里的 LocalDateTime 一致。
type Contact struct {
	ID          int64
	Name        string
	Sex         int
	Birthday    int64
	Description string
	Avatar      string
	CreateTime  int64
}

// Save 是新增和修改的入参。Avatar 为 nil 表示请求没带这个字段，修改时不能把原头像写成空。
type Save struct {
	ID          int64
	Name        string
	Sex         int
	Birthday    int64
	Description string
	Avatar      *string
}

// Query 是分页和导出共用的筛选。PageSize 小于 0 时不分页，给导出使用。
type Query struct {
	PageNo     int
	PageSize   int
	Name       string
	Sex        *int
	CreateFrom string
	CreateTo   string
}

// Page 是分页结果。空列表保持空切片，避免前端拿到 null。
type Page struct {
	List  []Contact
	Total int64
}
