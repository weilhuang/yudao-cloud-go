// Package logger 提供登录日志和操作日志。
// 管理后台用来查，其他 Java 服务通过 /rpc-api 写入。
package logger

// LoginLog 对应 system_login_log。时间是毫秒时间戳。
type LoginLog struct {
	ID         int64  `json:"id"`
	LogType    int    `json:"logType"`
	TraceID    string `json:"traceId"`
	UserID     int64  `json:"userId"`
	UserType   int    `json:"userType"`
	Username   string `json:"username"`
	Result     int    `json:"result"`
	UserIP     string `json:"userIp"`
	UserAgent  string `json:"userAgent"`
	CreateTime int64  `json:"createTime"`
}

// OperateLog 对应 system_operate_log。UserName 来自用户表，方便列表直接显示。
type OperateLog struct {
	ID            int64  `json:"id"`
	TraceID       string `json:"traceId"`
	UserID        int64  `json:"userId"`
	UserName      string `json:"userName"`
	UserType      int    `json:"userType"`
	Type          string `json:"type"`
	SubType       string `json:"subType"`
	BizID         int64  `json:"bizId"`
	Action        string `json:"action"`
	Extra         string `json:"extra"`
	RequestMethod string `json:"requestMethod"`
	RequestURL    string `json:"requestUrl"`
	UserIP        string `json:"userIp"`
	UserAgent     string `json:"userAgent"`
	CreateTime    int64  `json:"createTime"`
}

// LoginQuery 是登录日志分页条件。Success 为空表示不按结果过滤。
type LoginQuery struct {
	PageNo   int
	PageSize int
	UserIP   string
	Username string
	Success  *bool
	Start    *int64
	End      *int64
}

// OperateQuery 是操作日志分页条件。
type OperateQuery struct {
	PageNo   int
	PageSize int
	UserID   int64
	BizID    int64
	Type     string
	SubType  string
	Action   string
	Start    *int64
	End      *int64
}

// Page 是管理后台的分页结构。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}
