package auth

import "fmt"

// 与 Java ErrorCodeConstants、GlobalErrorCodeConstants 对齐。
const (
	codeBadCredentials = 1_002_000_000
	codeUserDisabled   = 1_002_000_001
	codeCaptcha        = 1_002_000_004
	codeBadRequest     = 400
	codeUnauthorized   = 401
	codeForbidden      = 403
	codeClientNotFound = 1_002_020_000
	codeClientDisabled = 1_002_020_002
)

// Error 是返回给前端的业务错误。HTTP 仍是 200，前端看 code。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string {
	return e.Msg
}

func badCredentials() *Error {
	return &Error{Code: codeBadCredentials, Msg: "登录失败，账号密码不正确"}
}

func userDisabled() *Error {
	return &Error{Code: codeUserDisabled, Msg: "登录失败，账号被禁用"}
}

func captchaMissing() *Error {
	return &Error{Code: codeCaptcha, Msg: "验证码不正确，原因：验证码不能为空"}
}

func badRequest(msg string) *Error {
	return &Error{Code: codeBadRequest, Msg: msg}
}

func unauthorized(msg string) *Error {
	return &Error{Code: codeUnauthorized, Msg: msg}
}

func forbidden(msg string) *Error {
	return &Error{Code: codeForbidden, Msg: msg}
}

func asError(err error) *Error {
	if err == nil {
		return nil
	}
	if biz, ok := err.(*Error); ok {
		return biz
	}
	return &Error{Code: 500, Msg: fmt.Sprintf("系统异常")}
}
