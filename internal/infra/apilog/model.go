// Package apilog 记录并查询 API 访问日志和错误日志。
// 表在 infra，但单体、system、infra 三个进程都会写，因为它们共用同一套库。
package apilog

import "context"

// AccessLog 对应 infra_api_access_log。时间是毫秒时间戳。
type AccessLog struct {
	ID              int64  `json:"id"`
	TraceID         string `json:"traceId"`
	UserID          int64  `json:"userId"`
	UserType        int    `json:"userType"`
	ApplicationName string `json:"applicationName"`
	RequestMethod   string `json:"requestMethod"`
	RequestURL      string `json:"requestUrl"`
	RequestParams   string `json:"requestParams"`
	ResponseBody    string `json:"responseBody"`
	UserIP          string `json:"userIp"`
	UserAgent       string `json:"userAgent"`
	OperateModule   string `json:"operateModule"`
	OperateName     string `json:"operateName"`
	OperateType     int    `json:"operateType"`
	BeginTime       int64  `json:"beginTime"`
	EndTime         int64  `json:"endTime"`
	Duration        int    `json:"duration"`
	ResultCode      int    `json:"resultCode"`
	ResultMsg       string `json:"resultMsg"`
	CreateTime      int64  `json:"createTime"`
}

// ErrorLog 对应 infra_api_error_log。ProcessStatus 0 未处理，1 已处理，2 已忽略。
type ErrorLog struct {
	ID                        int64  `json:"id"`
	TraceID                   string `json:"traceId"`
	UserID                    int64  `json:"userId"`
	UserType                  int    `json:"userType"`
	ApplicationName           string `json:"applicationName"`
	RequestMethod             string `json:"requestMethod"`
	RequestURL                string `json:"requestUrl"`
	RequestParams             string `json:"requestParams"`
	UserIP                    string `json:"userIp"`
	UserAgent                 string `json:"userAgent"`
	ExceptionTime             int64  `json:"exceptionTime"`
	ExceptionName             string `json:"exceptionName"`
	ExceptionMessage          string `json:"exceptionMessage"`
	ExceptionRootCauseMessage string `json:"exceptionRootCauseMessage"`
	ExceptionStackTrace       string `json:"exceptionStackTrace"`
	ExceptionClassName        string `json:"exceptionClassName"`
	ExceptionFileName         string `json:"exceptionFileName"`
	ExceptionMethodName       string `json:"exceptionMethodName"`
	ExceptionLineNumber       int    `json:"exceptionLineNumber"`
	ProcessStatus             int    `json:"processStatus"`
	ProcessTime               *int64 `json:"processTime"`
	ProcessUserID             int64  `json:"processUserId"`
	CreateTime                int64  `json:"createTime"`
}

// AccessQuery 是访问日志分页条件。Duration 有值时表示耗时大于等于它。
type AccessQuery struct {
	PageNo          int
	PageSize        int
	UserID          int64
	UserType        *int
	ApplicationName string
	RequestURL      string
	Start           *int64
	End             *int64
	Duration        *int
	ResultCode      *int
}

// ErrorQuery 是错误日志分页条件。
type ErrorQuery struct {
	PageNo          int
	PageSize        int
	UserID          int64
	UserType        *int
	ApplicationName string
	RequestURL      string
	Start           *int64
	End             *int64
	ProcessStatus   *int
}

// Page 是管理后台的分页结构。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// Error 是错误日志处理时的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Store 读写访问日志和错误日志。
type Store interface {
	CreateAccess(ctx context.Context, tenantID int64, item AccessLog) error
	AccessPage(ctx context.Context, tenantID int64, q AccessQuery) (Page[AccessLog], error)
	AccessByID(ctx context.Context, tenantID, id int64) (*AccessLog, error)
	CreateError(ctx context.Context, tenantID int64, item ErrorLog) error
	ErrorPage(ctx context.Context, tenantID int64, q ErrorQuery) (Page[ErrorLog], error)
	ErrorByID(ctx context.Context, tenantID, id int64) (*ErrorLog, error)
	UpdateErrorStatus(ctx context.Context, id, userID int64, status int) error
}
