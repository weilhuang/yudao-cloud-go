package directory

import (
	"context"

	"golang.org/x/crypto/bcrypt"
)

const (
	codeUsernameExists = 1_002_003_000
	codeUserNotExists  = 1_002_003_003
	codeBadRequest     = 400
	codeForbidden      = 403
)

// Error 是目录接口的业务错误。
type Error struct {
	Code  int
	Msg   string
	Cause error
}

func (e *Error) Error() string { return e.Msg }

// Unwrap 保留缓存失效等底层故障，响应仍只展示安全的业务提示。
func (e *Error) Unwrap() error { return e.Cause }

// Service 组合读和写。新密码默认使用 bcrypt 的成本 10；旧哈希会按自身成本验证。
type Service struct {
	Reader
	Writer  UserWriter
	Depts   DeptWriter
	Access  AccessStore
	Dicts   DictStore
	Tenants TenantStore
	// JavaRoleCache 只清理由 Java Spring Cache 写入的角色、授权和部门子树缓存。
	// 单体或 system 进程由 app 注入；未注入时适用于不与 Java 混跑的用例。
	JavaRoleCache JavaRoleCache
	BcryptCost    int
	// OnNotice 按公告所属租户推给在线管理员。为空时只保存，不推送。
	OnNotice func(tenantID int64, item Notice)
	// RevokeAdminTokens 在用户被禁用且状态已写入后，撤销该管理员的访问令牌和刷新令牌。
	// 由应用装配接到 auth.Service，避免目录包反向依赖令牌存储。为空时只改状态。
	RevokeAdminTokens func(ctx context.Context, tenantID, userID int64) error
	// DeptScope 计算当前管理员能看到的部门和本人范围。管理端用户读接口必须注入；
	// 为空时这些接口失败，避免退回成“同租户全量可见”。
	DeptScope func(ctx context.Context, tenantID, userID int64) (UserAccess, error)
}

func (s *Service) cost() int {
	if s.BcryptCost > 0 {
		return s.BcryptCost
	}
	return bcrypt.DefaultCost
}

// CreateUser 校验账号唯一后写入 BCrypt 哈希。
func (s *Service) CreateUser(ctx context.Context, tenantID int64, user UserSave) (int64, error) {
	if user.Username == "" || user.Password == "" || user.Nickname == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "用户账号、昵称、密码不能为空"}
	}
	taken, err := s.Writer.UsernameTaken(ctx, tenantID, user.Username, 0)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: codeUsernameExists, Msg: "用户账号已经存在"}
	}
	if err := s.validateUserPosts(ctx, tenantID, user.PostIDs); err != nil {
		return 0, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), s.cost())
	if err != nil {
		return 0, err
	}
	return s.Writer.CreateUser(ctx, tenantID, user, string(hash))
}

// UpdateUser 不改密码。
func (s *Service) UpdateUser(ctx context.Context, tenantID int64, user UserSave) error {
	current, err := s.Reader.UserGet(ctx, tenantID, user.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	taken, err := s.Writer.UsernameTaken(ctx, tenantID, user.Username, user.ID)
	if err != nil {
		return err
	}
	if taken {
		return &Error{Code: codeUsernameExists, Msg: "用户账号已经存在"}
	}
	if err := s.validateUserPosts(ctx, tenantID, user.PostIDs); err != nil {
		return err
	}
	return s.Writer.UpdateUser(ctx, tenantID, user)
}

// validateUserPosts 与 Java PostService.validatePostList 对齐：岗位必须属于当前租户且处于启用状态。
func (s *Service) validateUserPosts(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) > 0 && s.Access == nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		post, err := s.Access.PostByID(ctx, tenantID, id)
		if err != nil {
			return err
		}
		if post == nil {
			return &Error{Code: 1_002_005_000, Msg: "当前岗位不存在"}
		}
		if post.Status != 0 {
			return &Error{Code: 1_002_005_001, Msg: "岗位(" + post.Name + ") 不处于开启状态，不允许选择"}
		}
	}
	return nil
}

// ResetPassword 按用户编号重置密码。
func (s *Service) ResetPassword(ctx context.Context, tenantID, id int64, password string) error {
	if password == "" {
		return &Error{Code: codeBadRequest, Msg: "密码不能为空"}
	}
	current, err := s.Reader.UserGet(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost())
	if err != nil {
		return err
	}
	return s.Writer.UpdatePassword(ctx, tenantID, id, string(hash))
}

// UpdateStatus 启用或禁用用户。
func (s *Service) UpdateStatus(ctx context.Context, tenantID, id int64, status int) error {
	current, err := s.Reader.UserGet(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	if err := s.Writer.UpdateStatus(ctx, tenantID, id, status); err != nil {
		return err
	}
	// Java CommonStatusEnum.DISABLE 是 1。禁用后删除该用户的管理员令牌，
	// 避免混跑时 Java 仍用 Redis 里的旧访问令牌。
	if status == 1 && s.RevokeAdminTokens != nil {
		if err := s.RevokeAdminTokens(ctx, tenantID, id); err != nil {
			return &Error{Code: 500, Msg: "数据库已提交，但访问令牌撤销失败；请检查后补偿清理令牌", Cause: err}
		}
	}
	return nil
}

// DeleteUser 逻辑删除用户，并在同一事务里删除本租户的用户角色、用户岗位。
// 冻结 Java 允许删除当前登录用户，因此这里不再额外拦截。
func (s *Service) DeleteUser(ctx context.Context, tenantID, _, id int64) error {
	current, err := s.Reader.UserGet(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: codeUserNotExists, Msg: "用户不存在"}
	}
	if err := s.Writer.DeleteUser(ctx, tenantID, id); err != nil {
		return err
	}
	return s.evictUserRolesAfterWrite(ctx, id)
}

// DeleteUserList 批量逻辑删除。不存在的编号略过；只清理本次真正删除的用户缓存。
// 其它租户的同号关系不会被修改。任一缓存清理失败时，数据库已经提交。
func (s *Service) DeleteUserList(ctx context.Context, tenantID int64, ids []int64) error {
	deleted, err := s.Writer.DeleteUserList(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	if len(deleted) == 0 {
		return nil
	}
	return s.evictAfterWrite(ctx, "用户批量删除", func(cacheCtx context.Context) error {
		for _, id := range deleted {
			if err := s.JavaRoleCache.EvictUserRoles(cacheCtx, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveDept 创建或更新部门。父部门必须属于同一租户，更新不能形成部门环路。
func (s *Service) SaveDept(ctx context.Context, tenantID int64, dept Dept) (int64, error) {
	if dept.Name == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "部门名称不能为空"}
	}
	if dept.ID != 0 {
		ok, err := s.Depts.DeptExists(ctx, tenantID, dept.ID)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, &Error{Code: codeDeptNotFound, Msg: "当前部门不存在"}
		}
	}
	if err := s.validateDeptParent(ctx, tenantID, dept.ID, dept.ParentID); err != nil {
		return 0, err
	}
	if dept.ID == 0 {
		id, err := s.Depts.CreateDept(ctx, tenantID, dept)
		if err != nil {
			return 0, err
		}
		return id, s.EvictDeptChildrenAfterWrite(ctx, tenantID)
	}
	if err := s.Depts.UpdateDept(ctx, tenantID, dept); err != nil {
		return dept.ID, err
	}
	return dept.ID, s.EvictDeptChildrenAfterWrite(ctx, tenantID)
}
