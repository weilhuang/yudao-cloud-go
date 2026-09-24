package demo02

import (
	"context"
	"database/sql"
)

// Service 按租户维护示例分类树。父级 0 是根，不能把节点挂到自己或自己的子孙下面。
type Service struct {
	DB *sql.DB
}

func (s *Service) Create(ctx context.Context, tenantID int64, in Save) (int64, error) {
	if err := s.validateParent(ctx, tenantID, 0, in.ParentID); err != nil {
		return 0, err
	}
	if err := s.validateName(ctx, tenantID, 0, in.ParentID, in.Name); err != nil {
		return 0, err
	}
	return s.insert(ctx, tenantID, in)
}

func (s *Service) Update(ctx context.Context, tenantID int64, in Save) error {
	current, err := s.get(ctx, tenantID, in.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeNotExists, "示例分类不存在")
	}
	if err := s.validateParent(ctx, tenantID, in.ID, in.ParentID); err != nil {
		return err
	}
	if err := s.validateName(ctx, tenantID, in.ID, in.ParentID, in.Name); err != nil {
		return err
	}
	return s.update(ctx, tenantID, in)
}

func (s *Service) Delete(ctx context.Context, tenantID, id int64) error {
	current, err := s.get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if current == nil {
		return biz(codeNotExists, "示例分类不存在")
	}
	n, err := s.childCount(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return biz(codeHasChildren, "存在存在子示例分类，无法删除")
	}
	return s.softDelete(ctx, tenantID, id)
}

func (s *Service) Get(ctx context.Context, tenantID, id int64) (*Category, error) {
	return s.get(ctx, tenantID, id)
}

func (s *Service) List(ctx context.Context, tenantID int64, q Query) ([]Category, error) {
	return s.list(ctx, tenantID, q)
}

func (s *Service) validateParent(ctx context.Context, tenantID, id, parentID int64) error {
	if parentID == parentRoot {
		return nil
	}
	if id != 0 && id == parentID {
		return biz(codeParentSelf, "不能设置自己为父示例分类")
	}
	parent, err := s.get(ctx, tenantID, parentID)
	if err != nil {
		return err
	}
	if parent == nil {
		return biz(codeParentMissing, "父级示例分类不存在")
	}
	if id == 0 {
		return nil
	}
	for i := 0; i < 32767; i++ {
		parentID = parent.ParentID
		if id == parentID {
			return biz(codeParentIsChild, "不能设置自己的子示例分类为父示例分类")
		}
		if parentID == parentRoot {
			return nil
		}
		parent, err = s.get(ctx, tenantID, parentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return nil
		}
	}
	return nil
}

func (s *Service) validateName(ctx context.Context, tenantID, id, parentID int64, name string) error {
	current, err := s.byParentName(ctx, tenantID, parentID, name)
	if err != nil {
		return err
	}
	if current == nil || current.ID == id {
		return nil
	}
	return biz(codeNameDuplicate, "已经存在该名字的示例分类")
}
