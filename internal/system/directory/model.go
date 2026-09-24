// Package directory 提供管理后台首页之后立刻会拉的列表：字典、部门、用户、菜单、角色。
package directory

import "time"

// DictItem 对应字典精简接口，前端用来渲染下拉框。
type DictItem struct {
	DictType  string `json:"dictType"`
	Value     string `json:"value"`
	Label     string `json:"label"`
	ColorType string `json:"colorType"`
	CSSClass  string `json:"cssClass"`
}

// Dept 是部门列表和精简列表共用的字段。
type Dept struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	ParentID     int64  `json:"parentId"`
	Sort         int    `json:"sort"`
	LeaderUserID *int64 `json:"leaderUserId"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Status       int    `json:"status"`
	CreateTime   int64  `json:"createTime"`
}

// UserSimple 是用户下拉框。
type UserSimple struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Sex      *int   `json:"sex"`
	DeptID   *int64 `json:"deptId"`
	DeptName string `json:"deptName"`
}

// UserDetail 是用户分页和详情。
type UserDetail struct {
	ID         int64   `json:"id"`
	Username   string  `json:"username"`
	Nickname   string  `json:"nickname"`
	Remark     string  `json:"remark"`
	DeptID     *int64  `json:"deptId"`
	DeptName   string  `json:"deptName"`
	PostIDs    []int64 `json:"postIds"`
	Email      string  `json:"email"`
	Mobile     string  `json:"mobile"`
	Sex        *int    `json:"sex"`
	Avatar     string  `json:"avatar"`
	Status     int     `json:"status"`
	LoginIP    string  `json:"loginIp"`
	LoginDate  *int64  `json:"loginDate"`
	CreateTime int64   `json:"createTime"`
}

// MenuSimple 是菜单下拉框。
type MenuSimple struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID int64  `json:"parentId"`
	Type     int    `json:"type"`
}

// RoleSimple 是角色下拉框。
type RoleSimple struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// RoleDetail 对应角色详情和分页的 RoleRespVO。
type RoleDetail struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Sort      int    `json:"sort"`
	Status    int    `json:"status"`
	Type      int    `json:"type"`
	Remark    string `json:"remark"`
	DataScope int    `json:"dataScope"`
	// 指定部门的数据范围以 JSON 数组存于 system_role.data_scope_dept_ids。
	DataScopeDeptIDs []int64 `json:"dataScopeDeptIds"`
	CreateTime       int64   `json:"createTime"`
}

// DictData 是字典数据分页。
type DictData struct {
	ID         int64  `json:"id"`
	Sort       int    `json:"sort"`
	Label      string `json:"label"`
	Value      string `json:"value"`
	DictType   string `json:"dictType"`
	Status     int    `json:"status"`
	ColorType  string `json:"colorType"`
	CSSClass   string `json:"cssClass"`
	Remark     string `json:"remark"`
	CreateTime int64  `json:"createTime"`
}

// MenuDetail 是菜单管理列表，平铺返回，由前端组树。
type MenuDetail struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Permission    string `json:"permission"`
	Type          int    `json:"type"`
	Sort          int    `json:"sort"`
	ParentID      int64  `json:"parentId"`
	Path          string `json:"path"`
	Icon          string `json:"icon"`
	Component     string `json:"component"`
	ComponentName string `json:"componentName"`
	Status        int    `json:"status"`
	Visible       bool   `json:"visible"`
	KeepAlive     bool   `json:"keepAlive"`
	AlwaysShow    bool   `json:"alwaysShow"`
	CreateTime    int64  `json:"createTime"`
}

// PostSimple 是岗位下拉框。
type PostSimple struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// PostDetail 对应 Java PostRespVO；创建时间只属于读模型，不混入写入参数。
type PostDetail struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Code       string `json:"code"`
	Sort       int    `json:"sort"`
	Status     int    `json:"status"`
	Remark     string `json:"remark"`
	CreateTime int64  `json:"createTime"`
}

// PostQuery 对应 PostPageReqVO。PageSize=-1 仅作为导出调用的内部标记。
type PostQuery struct {
	PageNo   int
	PageSize int
	Code     string
	Name     string
	Status   *int
}

// Page 是芋道分页：list + total。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// UserQuery 是用户分页和导出的筛选条件。
type UserQuery struct {
	PageNo      int
	PageSize    int
	Username    string
	Mobile      string
	Status      *int
	DeptID      *int64
	DeptIDs     []int64
	RoleID      *int64
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	// Access 是当前操作者的部门/本人数据范围。nil 表示这条查询不在管理请求里。
	Access *UserAccess
}

// UserAccess 对齐 Java DeptDataPermissionRespDTO。All 为真时不再追加条件。
type UserAccess struct {
	All     bool
	Self    bool
	UserID  int64
	DeptIDs []int64
}

// UserSave 是创建和修改用户的入参。
type UserSave struct {
	ID       int64
	Username string
	Nickname string
	Password string
	DeptID   *int64
	PostIDs  []int64 `json:"postIds"`
	Email    string
	Mobile   string
	Sex      *int
	Avatar   string
	Remark   string
}
