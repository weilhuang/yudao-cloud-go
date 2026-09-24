package directory

import (
	"context"
	"testing"
)

func TestDeleteDictTypeBlockedByData(t *testing.T) {
	svc := &Service{Dicts: &memDicts{typ: &DictType{ID: 1, Type: "sys_sex", Status: 0}, dataCount: 2}}
	err := svc.DeleteDictType(context.Background(), 1)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_006_005 {
		t.Fatal(err)
	}
}

func TestSaveDictDataRejectsDisabledType(t *testing.T) {
	svc := &Service{Dicts: &memDicts{typ: &DictType{ID: 1, Type: "sys_sex", Status: 1}}}
	_, err := svc.SaveDictData(context.Background(), DictData{Label: "男", Value: "1", DictType: "sys_sex"})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_006_002 {
		t.Fatal(err)
	}
}

func TestSaveDictDataRejectsDuplicateValue(t *testing.T) {
	svc := &Service{Dicts: &memDicts{typ: &DictType{Type: "sys_sex", Status: 0}, valueTaken: true}}
	_, err := svc.SaveDictData(context.Background(), DictData{Label: "男", Value: "1", DictType: "sys_sex"})
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_002_007_003 {
		t.Fatal(err)
	}
}

type memDicts struct {
	typ        *DictType
	dataCount  int
	valueTaken bool
	renamed    string
}

func (m *memDicts) DictTypeByID(context.Context, int64) (*DictType, error) { return m.typ, nil }
func (m *memDicts) DictTypeByType(context.Context, string) (*DictType, error) {
	return m.typ, nil
}
func (m *memDicts) DictTypeNameTaken(context.Context, string, int64) (bool, error) { return false, nil }
func (m *memDicts) DictTypeTaken(context.Context, string, int64) (bool, error)     { return false, nil }
func (m *memDicts) DictTypeDataCount(context.Context, string) (int, error)         { return m.dataCount, nil }
func (m *memDicts) CreateDictType(context.Context, DictType) (int64, error)        { return 1, nil }
func (m *memDicts) UpdateDictType(_ context.Context, item DictType, previous string) error {
	m.renamed = previous + "->" + item.Type
	return nil
}
func (m *memDicts) DeleteDictType(context.Context, int64) error       { return nil }
func (m *memDicts) DeleteDictTypeList(context.Context, []int64) error { return nil }
func (m *memDicts) DictTypePage(context.Context, DictTypeQuery) (Page[DictType], error) {
	return Page[DictType]{}, nil
}
func (m *memDicts) DictTypeExportRows(context.Context, DictTypeQuery, func(DictType) error) error {
	return nil
}
func (m *memDicts) DictTypeSimple(context.Context) ([]DictType, error) { return nil, nil }
func (m *memDicts) DictDataByID(context.Context, int64) (*DictData, error) {
	return &DictData{ID: 1}, nil
}
func (m *memDicts) DictValueTaken(context.Context, string, string, int64) (bool, error) {
	return m.valueTaken, nil
}
func (m *memDicts) CreateDictData(context.Context, DictData) (int64, error) { return 1, nil }
func (m *memDicts) UpdateDictData(context.Context, DictData) error          { return nil }
func (m *memDicts) DeleteDictData(context.Context, int64) error             { return nil }
func (m *memDicts) DeleteDictDataList(context.Context, []int64) error       { return nil }
func (m *memDicts) DictDataPage(context.Context, DictDataQuery) (Page[DictData], error) {
	return Page[DictData]{}, nil
}
func (m *memDicts) DictDataExportRows(context.Context, string, string, *int, func(DictData) error) error {
	return nil
}
