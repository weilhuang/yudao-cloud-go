// Package rolepostrpc 提供岗位和角色的 Feign 兼容接口。
// 业务校验与 HTTP 解析分开，方便直接验证 Java 接口的返回语义。
package rolepostrpc

import (
	"context"
	"fmt"
)

// Post 与 Java PostRespDTO 对齐；不把数据库的备注、租户编号等字段泄露给 RPC 调用方。
type Post struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Sort   int    `json:"sort"`
	Status int    `json:"status"`
}

// Role 与 Java RoleRespDTO 对齐。
type Role struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Sort   int    `json:"sort"`
	Status int    `json:"status"`
}

// Reader 必须只返回当前租户且未逻辑删除的记录；停用记录仍要返回，交由 valid 校验。
type Reader interface {
	Posts(ctx context.Context, tenantID int64, ids []int64) ([]Post, error)
	Roles(ctx context.Context, tenantID int64, ids []int64) ([]Role, error)
}

// Error 对应 CommonResult 的业务错误，不改变 HTTP 200 的 Feign 错误封装方式。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

type Service struct{ Reader Reader }

func (s *Service) PostList(ctx context.Context, tenantID int64, ids []int64) ([]Post, error) {
	if len(ids) == 0 {
		return []Post{}, nil
	}
	posts, err := s.Reader.Posts(ctx, tenantID, ids)
	if posts == nil && err == nil {
		posts = []Post{}
	}
	return posts, err
}

func (s *Service) RoleList(ctx context.Context, tenantID int64, ids []int64) ([]Role, error) {
	if len(ids) == 0 {
		return []Role{}, nil
	}
	roles, err := s.Reader.Roles(ctx, tenantID, ids)
	if roles == nil && err == nil {
		roles = []Role{}
	}
	return roles, err
}

// RoleGet 与 Java getRole 一样：不存在时返回 null，停用时仍返回详情。
func (s *Service) RoleGet(ctx context.Context, tenantID, id int64) (*Role, error) {
	roles, err := s.Reader.Roles(ctx, tenantID, []int64{id})
	if err != nil || len(roles) == 0 {
		return nil, err
	}
	return &roles[0], nil
}

// ValidPostList 按请求顺序逐项校验，确保缺失和停用同时存在时错误与 Java 一致。
func (s *Service) ValidPostList(ctx context.Context, tenantID int64, ids []int64) error {
	posts, err := s.PostList(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	byID := make(map[int64]Post, len(posts))
	for _, post := range posts {
		byID[post.ID] = post
	}
	for _, id := range ids {
		post, ok := byID[id]
		if !ok {
			return &Error{Code: 1_002_005_000, Msg: "当前岗位不存在"}
		}
		if post.Status != 0 {
			return &Error{Code: 1_002_005_001, Msg: fmt.Sprintf("岗位(%s) 不处于开启状态，不允许选择", post.Name)}
		}
	}
	return nil
}

// ValidRoleList 与 Java RoleService.validateRoleList 使用相同的状态和错误规则。
func (s *Service) ValidRoleList(ctx context.Context, tenantID int64, ids []int64) error {
	roles, err := s.RoleList(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	byID := make(map[int64]Role, len(roles))
	for _, role := range roles {
		byID[role.ID] = role
	}
	for _, id := range ids {
		role, ok := byID[id]
		if !ok {
			return &Error{Code: 1_002_002_000, Msg: "角色不存在"}
		}
		if role.Status != 0 {
			return &Error{Code: 1_002_002_004, Msg: fmt.Sprintf("名字为【%s】的角色已被禁用", role.Name)}
		}
	}
	return nil
}
