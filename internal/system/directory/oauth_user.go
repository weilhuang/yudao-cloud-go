package directory

import (
	"net/mail"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// oauthUserGet 只认访问令牌上的 user.read，不走菜单权限。没有岗位时 posts 为 null，与 Java 空集合语义一致。
func (h *handler) oauthUserGet(c *gin.Context) {
	who, ok := h.oauthCaller(c, "user.read")
	if !ok {
		return
	}
	user, err := h.svc.UserGet(c.Request.Context(), who.tenantID, who.userID)
	if err != nil {
		writeErr(c, err)
		return
	}
	if user == nil {
		writeErr(c, &Error{Code: codeUserNotExists, Msg: "用户不存在"})
		return
	}
	var dept any
	if user.DeptID != nil {
		dept = gin.H{"id": *user.DeptID, "name": user.DeptName}
	}
	var posts any
	if len(user.PostIDs) > 0 {
		found, err := h.svc.PostsByIDs(c.Request.Context(), who.tenantID, user.PostIDs)
		if err != nil {
			writeErr(c, err)
			return
		}
		if found == nil {
			found = []PostSimple{}
		}
		posts = found
	}
	if user.DeptID != nil && user.DeptName == "" {
		dept = nil
	}
	httpx.OK(c, gin.H{
		"id": user.ID, "username": user.Username, "nickname": user.Nickname,
		"email": user.Email, "mobile": user.Mobile, "sex": user.Sex, "avatar": user.Avatar,
		"dept": dept, "posts": posts,
	})
}

// oauthUserUpdate 需要 user.write。未出现在 JSON 里的字段保持原值，头像不在这个接口的写入范围。
func (h *handler) oauthUserUpdate(c *gin.Context) {
	who, ok := h.oauthCaller(c, "user.write")
	if !ok {
		return
	}
	var req struct {
		Nickname *string `json:"nickname"`
		Email    *string `json:"email"`
		Mobile   *string `json:"mobile"`
		Sex      *int    `json:"sex"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if err := validateOAuthUser(req.Nickname, req.Email, req.Mobile); err != nil {
		writeErr(c, err)
		return
	}
	if err := h.svc.Tenants.UpdateOAuthUser(c.Request.Context(), who.tenantID, who.userID, req.Nickname, req.Email, req.Mobile, req.Sex); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) oauthCaller(c *gin.Context, scope string) (caller, bool) {
	token, err := h.sessions.Check(c.Request.Context(), bearer(c))
	if err != nil {
		writeErr(c, err)
		return caller{}, false
	}
	if token.UserType != 2 || token.UserID <= 0 {
		writeErr(c, &Error{Code: 403, Msg: "您无权访问管理后台"})
		return caller{}, false
	}
	header, ok := headerTenant(c)
	if !ok {
		writeErr(c, &Error{Code: 400, Msg: "请求的租户标识未传递，请进行排查"})
		return caller{}, false
	}
	if header != token.TenantID {
		writeErr(c, &Error{Code: 403, Msg: "您无权访问该租户的数据"})
		return caller{}, false
	}
	if !scopeAllowed(token.Scopes, scope) {
		writeErr(c, &Error{Code: 403, Msg: "没有该操作权限"})
		return caller{}, false
	}
	return caller{userID: token.UserID, tenantID: token.TenantID}, true
}

func scopeAllowed(scopes []string, want string) bool {
	for _, scope := range scopes {
		if scope == want {
			return true
		}
	}
	return false
}

// validateOAuthUser 对齐 OAuth2UserUpdateReqVO：未传字段跳过；空手机号不通过；空邮箱视为未填写。
func validateOAuthUser(nickname, email, mobile *string) error {
	if nickname != nil && utf8.RuneCountInString(*nickname) > 30 {
		return &Error{Code: 400, Msg: "用户昵称长度不能超过 30 个字符"}
	}
	if email != nil {
		if utf8.RuneCountInString(*email) > 50 {
			return &Error{Code: 400, Msg: "邮箱长度不能超过 50 个字符"}
		}
		if *email != "" {
			if _, err := mail.ParseAddress(*email); err != nil {
				return &Error{Code: 400, Msg: "邮箱格式不正确"}
			}
		}
	}
	if mobile != nil && utf8.RuneCountInString(*mobile) != 11 {
		return &Error{Code: 400, Msg: "手机号长度必须 11 位"}
	}
	return nil
}
