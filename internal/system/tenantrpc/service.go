// Package tenantrpc 实现 Java TenantCommonApi 的内部 Feign 契约。
package tenantrpc

import (
	"context"
	"fmt"
	"time"
)

// Tenant 只保存租户校验所需的字段，避免把管理后台 DTO 暴露给内部 RPC。
type Tenant struct {
	ID         int64
	Name       string
	Status     int
	ExpireTime time.Time
}

// Reader 读取不受当前 tenant-id 限制的租户数据，对齐 Java 的 @TenantIgnore。
type Reader interface {
	TenantIDs(ctx context.Context) ([]int64, error)
	TenantByID(ctx context.Context, id int64) (*Tenant, error)
}

// Service 承载 TenantCommonApi 的两项业务操作。
type Service struct{ Reader Reader }

// TenantIDList 返回所有未逻辑删除的租户编号，包含停用和过期租户。
func (s *Service) TenantIDList(ctx context.Context) ([]int64, error) {
	ids, err := s.Reader.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return []int64{}, nil
	}
	return ids, nil
}

// ValidTenant 与 Java TenantServiceImpl.validTenant 保持相同的校验顺序。
// 到期判断使用时间原值，不丢弃 DATETIME 的精度；恰好到期的一刻仍然有效。
func (s *Service) ValidTenant(ctx context.Context, id int64, now time.Time) error {
	tenant, err := s.Reader.TenantByID(ctx, id)
	if err != nil {
		return err
	}
	if tenant == nil {
		return &Error{Code: 1_002_015_000, Msg: "租户不存在"}
	}
	if tenant.Status == 1 {
		return &Error{Code: 1_002_015_001, Msg: fmt.Sprintf("名字为【%s】的租户已被禁用", tenant.Name)}
	}
	if now.After(tenant.ExpireTime) {
		return &Error{Code: 1_002_015_002, Msg: fmt.Sprintf("名字为【%s】的租户已过期", tenant.Name)}
	}
	return nil
}

// Error 是可以通过 CommonResult 返回的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
