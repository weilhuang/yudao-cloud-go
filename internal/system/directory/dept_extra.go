package directory

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

// GetDept 读取当前租户的部门。管理端请求带数据范围时，范围外的部门按不存在返回。
func (s *Service) GetDept(ctx context.Context, tenantID, id int64) (*Dept, error) {
	dept, err := s.Reader.DeptGet(ctx, tenantID, id)
	if err != nil || dept == nil || deptVisible(accessFrom(ctx), dept.ID) {
		return dept, err
	}
	return nil, nil
}

// DeptList 返回部门列表。部门表只按 id 套数据范围，本人范围不会额外放出部门。
func (s *Service) DeptList(ctx context.Context, tenantID int64) ([]Dept, error) {
	list, err := s.Reader.DeptList(ctx, tenantID)
	if err != nil || list == nil {
		return list, err
	}
	access := accessFrom(ctx)
	if access == nil || access.All {
		return list, nil
	}
	out := make([]Dept, 0, len(list))
	for _, dept := range list {
		if deptVisible(access, dept.ID) {
			out = append(out, dept)
		}
	}
	return out, nil
}

// deptVisible 对齐部门表的数据权限：列是 id，没有用户列。
func deptVisible(access *UserAccess, id int64) bool {
	if access == nil || access.All {
		return true
	}
	for _, deptID := range access.DeptIDs {
		if deptID == id {
			return true
		}
	}
	return false
}

// DeleteDept 把 Java 的存在性与子部门校验交给同一个存储事务完成。
func (s *Service) DeleteDept(ctx context.Context, tenantID, id int64) error {
	if err := s.Depts.DeleteDept(ctx, tenantID, id); err != nil {
		return err
	}
	// Java 按部门 ID 缓存子节点；事务提交后须同步失效共享 Redis 中的条目。
	return s.EvictDeptChildrenAfterWrite(ctx, tenantID)
}

// DeleteDeptList 在同一事务里完成全量预检和批量软删除。
func (s *Service) DeleteDeptList(ctx context.Context, tenantID int64, ids []int64) error {
	if err := s.Depts.DeleteDeptList(ctx, tenantID, ids); err != nil {
		return err
	}
	return s.EvictDeptChildrenAfterWrite(ctx, tenantID)
}

// MountDeptExtras 补齐固定 Vben 部门页调用的三个端点；由 directory.Mount 调用。
func MountDeptExtras(r *gin.Engine, sessions *auth.Service, svc *Service) {
	h := &handler{sessions: sessions, svc: svc}
	dept := r.Group("/admin-api/system/dept")
	dept.GET("/get", h.permit("system:dept:query", h.scoped(h.deptGet)))
	dept.DELETE("/delete", h.permit("system:dept:delete", h.deptDelete))
	dept.DELETE("/delete-list", h.permit("system:dept:delete", h.deptDeleteList))
}

func (h *handler) deptGet(c *gin.Context, who caller) {
	id, ok := rpcID(c)
	if !ok {
		return
	}
	dept, err := h.svc.GetDept(c.Request.Context(), who.tenantID, id)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, dept)
}

func (h *handler) deptDelete(c *gin.Context, who caller) {
	id, ok := rpcID(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteDept(c.Request.Context(), who.tenantID, id); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) deptDeleteList(c *gin.Context, who caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if err := h.svc.DeleteDeptList(c.Request.Context(), who.tenantID, ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}
