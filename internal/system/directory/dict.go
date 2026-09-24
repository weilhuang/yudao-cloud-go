package directory

import (
	"context"
	"time"
)

// DictType 是字典类型。
type DictType struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     int    `json:"status"`
	Remark     string `json:"remark"`
	CreateTime int64  `json:"createTime,omitempty"`
}

// DictTypeQuery 保留 Java 字典类型分页和导出共用的筛选条件。
// CreateStart/End 对应 createTime 两端点，导出时只忽略分页，不丢筛选。
type DictTypeQuery struct {
	PageNo      int
	PageSize    int
	Name        string
	Type        string
	Status      *int
	CreateStart *time.Time
	CreateEnd   *time.Time
}

// DictDataQuery 对齐 Java 的字典数据分页过滤与排序。
type DictDataQuery struct {
	PageNo   int
	PageSize int
	Label    string
	DictType string
	Status   *int
}

// DictStore 维护字典类型和字典数据。字典不按租户隔离。
type DictStore interface {
	DictTypeByID(ctx context.Context, id int64) (*DictType, error)
	DictTypeByType(ctx context.Context, typ string) (*DictType, error)
	DictTypeNameTaken(ctx context.Context, name string, exceptID int64) (bool, error)
	DictTypeTaken(ctx context.Context, typ string, exceptID int64) (bool, error)
	DictTypeDataCount(ctx context.Context, typ string) (int, error)
	CreateDictType(ctx context.Context, item DictType) (int64, error)
	UpdateDictType(ctx context.Context, item DictType, previousType string) error
	DeleteDictType(ctx context.Context, id int64) error
	DeleteDictTypeList(ctx context.Context, ids []int64) error
	DictTypePage(ctx context.Context, query DictTypeQuery) (Page[DictType], error)
	DictTypeExportRows(ctx context.Context, query DictTypeQuery, emit func(DictType) error) error
	DictTypeSimple(ctx context.Context) ([]DictType, error)

	DictDataByID(ctx context.Context, id int64) (*DictData, error)
	DictValueTaken(ctx context.Context, dictType, value string, exceptID int64) (bool, error)
	CreateDictData(ctx context.Context, item DictData) (int64, error)
	UpdateDictData(ctx context.Context, item DictData) error
	DeleteDictData(ctx context.Context, id int64) error
	DeleteDictDataList(ctx context.Context, ids []int64) error
	DictDataPage(ctx context.Context, query DictDataQuery) (Page[DictData], error)
	DictDataExportRows(ctx context.Context, label, dictType string, status *int, emit func(DictData) error) error
}

// SaveDictType 创建或修改字典类型。类型下还有数据时不能删除，改类型编码时同步字典数据。
func (s *Service) SaveDictType(ctx context.Context, item DictType) (int64, error) {
	if item.Name == "" || item.Type == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "字典名称和类型不能为空"}
	}
	var previous string
	if item.ID != 0 {
		current, err := s.Dicts.DictTypeByID(ctx, item.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_006_001, Msg: "当前字典类型不存在"}
		}
		previous = current.Type
	}
	if taken, err := s.Dicts.DictTypeNameTaken(ctx, item.Name, item.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_006_003, Msg: "已经存在该名字的字典类型"}
	}
	if taken, err := s.Dicts.DictTypeTaken(ctx, item.Type, item.ID); err != nil || taken {
		if err != nil {
			return 0, err
		}
		return 0, &Error{Code: 1_002_006_004, Msg: "已经存在该类型的字典类型"}
	}
	if item.ID == 0 {
		return s.Dicts.CreateDictType(ctx, item)
	}
	return item.ID, s.Dicts.UpdateDictType(ctx, item, previous)
}

// DeleteDictType 类型下还有数据时拒绝删除。
func (s *Service) DeleteDictType(ctx context.Context, id int64) error {
	current, err := s.Dicts.DictTypeByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_006_001, Msg: "当前字典类型不存在"}
	}
	n, err := s.Dicts.DictTypeDataCount(ctx, current.Type)
	if err != nil {
		return err
	}
	if n > 0 {
		return &Error{Code: 1_002_006_005, Msg: "无法删除，该字典类型还有字典数据"}
	}
	return s.Dicts.DeleteDictType(ctx, id)
}

// DeleteDictTypeList 由存储层在事务中检查所有类型的子项后批量删除，避免部分成功。
func (s *Service) DeleteDictTypeList(ctx context.Context, ids []int64) error {
	return s.Dicts.DeleteDictTypeList(ctx, ids)
}

// SaveDictData 创建或修改字典数据。类型必须存在且开启，同一类型下值不能重复。
func (s *Service) SaveDictData(ctx context.Context, item DictData) (int64, error) {
	if item.Label == "" || item.Value == "" || item.DictType == "" {
		return 0, &Error{Code: codeBadRequest, Msg: "字典标签、键值和类型不能为空"}
	}
	typ, err := s.Dicts.DictTypeByType(ctx, item.DictType)
	if err != nil {
		return 0, err
	}
	if typ == nil {
		return 0, &Error{Code: 1_002_006_001, Msg: "当前字典类型不存在"}
	}
	if typ.Status != 0 {
		return 0, &Error{Code: 1_002_006_002, Msg: "字典类型不处于开启状态，不允许选择"}
	}
	if item.ID != 0 {
		current, err := s.Dicts.DictDataByID(ctx, item.ID)
		if err != nil {
			return 0, err
		}
		if current == nil {
			return 0, &Error{Code: 1_002_007_001, Msg: "当前字典数据不存在"}
		}
	}
	taken, err := s.Dicts.DictValueTaken(ctx, item.DictType, item.Value, item.ID)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, &Error{Code: 1_002_007_003, Msg: "已经存在该值的字典数据"}
	}
	if item.ID == 0 {
		return s.Dicts.CreateDictData(ctx, item)
	}
	return item.ID, s.Dicts.UpdateDictData(ctx, item)
}

// DeleteDictData 逻辑删除一条字典数据。
func (s *Service) DeleteDictData(ctx context.Context, id int64) error {
	current, err := s.Dicts.DictDataByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_002_007_001, Msg: "当前字典数据不存在"}
	}
	return s.Dicts.DeleteDictData(ctx, id)
}

// DeleteDictDataList 与 Java 批删一致，缺失编号静默跳过；一条 SQL 保证原子性。
func (s *Service) DeleteDictDataList(ctx context.Context, ids []int64) error {
	return s.Dicts.DeleteDictDataList(ctx, ids)
}
