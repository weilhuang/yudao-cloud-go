package directory

import "context"

// Reader 读取管理后台列表。字典和菜单在 Java 里忽略租户，这里也不按租户过滤。
type Reader interface {
	DictSimple(ctx context.Context) ([]DictItem, error)
	// DictEnabledByType 只返回指定类型里启用的字典，供 App 免登录查询。
	DictEnabledByType(ctx context.Context, dictType string) ([]DictData, error)
	DeptList(ctx context.Context, tenantID int64) ([]Dept, error)
	DeptGet(ctx context.Context, tenantID, id int64) (*Dept, error)
	UserSimple(ctx context.Context, tenantID int64) ([]UserSimple, error)
	UserPage(ctx context.Context, tenantID int64, query UserQuery) (Page[UserDetail], error)
	UserGet(ctx context.Context, tenantID, id int64) (*UserDetail, error)
	UserListByIDs(ctx context.Context, tenantID int64, ids []int64) ([]UserDetail, error)
	UserByNickname(ctx context.Context, tenantID int64, nickname string) ([]UserSimple, error)
	UserExportRows(ctx context.Context, tenantID int64, query UserQuery, emit func(UserDetail) error) error
	RoleExportRows(ctx context.Context, tenantID int64, name, code string, status *int, emit func(RoleDetail) error) error
	DictLabels(ctx context.Context, dictType string) (map[int]string, error)
	MenuSimple(ctx context.Context) ([]MenuSimple, error)
	MenuList(ctx context.Context, name string, status *int) ([]MenuDetail, error)
	RoleSimple(ctx context.Context, tenantID int64) ([]RoleSimple, error)
	RoleGet(ctx context.Context, tenantID, id int64) (*RoleDetail, error)
	RolePage(ctx context.Context, tenantID int64, pageNo, pageSize int, name, code string, status *int) (Page[RoleDetail], error)
	PostSimple(ctx context.Context, tenantID int64) ([]PostSimple, error)
	PostGet(ctx context.Context, tenantID, id int64) (*PostDetail, error)
	PostPage(ctx context.Context, tenantID int64, query PostQuery) (Page[PostDetail], error)
	PostExportRows(ctx context.Context, tenantID int64, query PostQuery, emit func(PostDetail) error) error
	PostStatusLabels(ctx context.Context) (map[int]string, error)
	PostsByIDs(ctx context.Context, tenantID int64, ids []int64) ([]PostSimple, error)
}

// DeptWriter 维护部门。parentID 为 0 表示根部门。
type DeptWriter interface {
	DeptExists(ctx context.Context, tenantID, id int64) (bool, error)
	CreateDept(ctx context.Context, tenantID int64, dept Dept) (int64, error)
	UpdateDept(ctx context.Context, tenantID int64, dept Dept) error
	DeleteDept(ctx context.Context, tenantID, id int64) error
	DeleteDeptList(ctx context.Context, tenantID int64, ids []int64) error
}

// UserWriter 维护用户。密码只在创建和重置时写入。
type UserWriter interface {
	UsernameTaken(ctx context.Context, tenantID int64, username string, exceptID int64) (bool, error)
	CreateUser(ctx context.Context, tenantID int64, user UserSave, passwordHash string) (int64, error)
	UpdateUser(ctx context.Context, tenantID int64, user UserSave) error
	UpdatePassword(ctx context.Context, tenantID, id int64, passwordHash string) error
	UpdateStatus(ctx context.Context, tenantID, id int64, status int) error
	DeleteUser(ctx context.Context, tenantID, id int64) error
	// DeleteUserList 返回本次在本租户实际删除的用户编号，供提交后清理缓存。
	DeleteUserList(ctx context.Context, tenantID int64, ids []int64) ([]int64, error)
	// ImportUsers 在一个事务里创建或更新。校验失败的行不写入；数据库错误回滚整批。
	ImportUsers(ctx context.Context, tenantID int64, rows []ImportUser, updateSupport bool, passwordHash func(string) (string, error)) (ImportResult, error)
}
