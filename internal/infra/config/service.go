package config

import "context"

// Store 读写参数配置。
type Store interface {
	ByID(ctx context.Context, id int64) (*Item, error)
	ByKey(ctx context.Context, key string) (*Item, error)
	Page(ctx context.Context, pageNo, pageSize int, name, key string, typ *int, start, end *int64) (Page[Item], error)
	Create(ctx context.Context, item Item) (int64, error)
	Update(ctx context.Context, item Item) error
	Delete(ctx context.Context, id int64) error
}

// Save 创建或修改。新建时类型固定为自定义，避免前端把内置配置标成可删。
func Save(ctx context.Context, store Store, item Item) (int64, error) {
	if item.Category == "" || item.Name == "" || item.Key == "" {
		return 0, &Error{Code: 400, Msg: "参数分类、名称和键名不能为空"}
	}
	current, err := store.ByKey(ctx, item.Key)
	if err != nil {
		return 0, err
	}
	if current != nil && current.ID != item.ID {
		return 0, &Error{Code: 1_001_000_002, Msg: "参数配置 key 重复"}
	}
	if item.ID == 0 {
		item.Type = typeCustom
		return store.Create(ctx, item)
	}
	existing, err := store.ByID(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return 0, &Error{Code: 1_001_000_001, Msg: "参数配置不存在"}
	}
	item.Type = existing.Type
	return item.ID, store.Update(ctx, item)
}

// Remove 不能删除系统内置参数。
func Remove(ctx context.Context, store Store, id int64) error {
	current, err := store.ByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_001_000_001, Msg: "参数配置不存在"}
	}
	if current.Type == typeSystem {
		return &Error{Code: 1_001_000_003, Msg: "不能删除类型为系统内置的参数配置"}
	}
	return store.Delete(ctx, id)
}

// VisibleValue 给管理端按键取值。不可见的配置不返回给前端。
func VisibleValue(ctx context.Context, store Store, key string) (string, bool, error) {
	current, err := store.ByKey(ctx, key)
	if err != nil || current == nil {
		return "", false, err
	}
	if !current.Visible {
		return "", false, &Error{Code: 1_001_000_004, Msg: "获取参数配置失败，原因：不允许获取不可见配置"}
	}
	return current.Value, true, nil
}
