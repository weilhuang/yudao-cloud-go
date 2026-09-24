package demo03

const (
	codeStudentMissing = 1_001_201_007
	codeCourseMissing  = 1_001_201_008
	codeGradeMissing   = 1_001_201_009
	codeGradeExists    = 1_001_201_010
)

// Error 是学生示例接口的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func biz(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

// Student 是 yudao_demo03_student 的一行。
type Student struct {
	ID          int64
	Name        string
	Sex         int
	Birthday    int64
	Description string
	CreateTime  int64
}

// Course 是学生课程。编号为 0 表示新增。
type Course struct {
	ID         int64
	StudentID  int64
	Name       string
	Score      int
	CreateTime int64
}

// Grade 是学生班级。一个学生在 ERP 新建时只能有一条。
type Grade struct {
	ID         int64
	StudentID  int64
	Name       string
	Teacher    string
	CreateTime int64
}

// Save 是学生主表字段。课程和班级只在标准和内嵌模式里随学生一起提交。
type Save struct {
	ID          int64
	Name        string
	Sex         int
	Birthday    int64
	Description string
	Courses     []Course
	Grade       *Grade
	WithChild   bool
}

// Query 是学生分页和导出的筛选。PageSize 小于 0 时不分页。
type Query struct {
	PageNo      int
	PageSize    int
	Name        string
	Sex         *int
	Description string
	CreateFrom  string
	CreateTo    string
}

// Page 是分页结果。
type Page[T any] struct {
	List  []T
	Total int64
}
