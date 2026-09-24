package demo01

import (
	"context"
	"database/sql"
)

// Service 按租户读写示例联系人。表没有忽略租户，所以每个查询都带 tenant_id。
type Service struct {
	DB *sql.DB
}

func (s *Service) Create(ctx context.Context, tenantID int64, in Save) (int64, error) {
	return s.insert(ctx, tenantID, in)
}

func (s *Service) Update(ctx context.Context, tenantID int64, in Save) error {
	current, err := s.get(ctx, tenantID, in.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return missing()
	}
	return s.update(ctx, tenantID, in)
}

func (s *Service) Delete(ctx context.Context, tenantID, id int64) error {
	return s.DeleteList(ctx, tenantID, []int64{id})
}

// DeleteList 要求每个编号都存在，重复编号也视为不存在。任一缺失则整批不删。
func (s *Service) DeleteList(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return missing()
	}
	found, err := s.existingIDs(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	if len(found) != len(ids) {
		return missing()
	}
	for _, id := range ids {
		if !found[id] {
			return missing()
		}
	}
	return s.softDelete(ctx, tenantID, ids)
}

func (s *Service) Get(ctx context.Context, tenantID, id int64) (*Contact, error) {
	return s.get(ctx, tenantID, id)
}

func (s *Service) Page(ctx context.Context, tenantID int64, q Query) (Page, error) {
	if q.PageNo <= 0 {
		q.PageNo = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 10
	}
	return s.page(ctx, tenantID, q)
}
