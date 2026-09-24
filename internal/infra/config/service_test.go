package config

import (
	"context"
	"testing"
)

func TestSaveCreateIsCustom(t *testing.T) {
	store := &memStore{}
	id, err := Save(context.Background(), store, Item{Category: "biz", Name: "开关", Key: "demo.flag", Value: "1", Visible: true})
	if err != nil || id != 9 || store.created.Type != typeCustom {
		t.Fatal(id, err, store.created.Type)
	}
}

func TestSaveRejectsDuplicateKey(t *testing.T) {
	store := &memStore{byKey: &Item{ID: 1, Key: "demo.flag"}}
	_, err := Save(context.Background(), store, Item{Category: "biz", Name: "开关", Key: "demo.flag"})
	assertCode(t, err, 1_001_000_002)
}

func TestRemoveBlocksSystemType(t *testing.T) {
	store := &memStore{byID: &Item{ID: 2, Type: typeSystem}}
	assertCode(t, Remove(context.Background(), store, 2), 1_001_000_003)
}

func TestVisibleValueHidesInvisible(t *testing.T) {
	store := &memStore{byKey: &Item{Key: "secret", Value: "x", Visible: false}}
	_, _, err := VisibleValue(context.Background(), store, "secret")
	assertCode(t, err, 1_001_000_004)
}

func assertCode(t *testing.T, err error, code int) {
	t.Helper()
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != code {
		t.Fatal(err)
	}
}

type memStore struct {
	byID    *Item
	byKey   *Item
	created Item
}

func (m *memStore) ByID(context.Context, int64) (*Item, error)   { return m.byID, nil }
func (m *memStore) ByKey(context.Context, string) (*Item, error) { return m.byKey, nil }
func (m *memStore) Page(context.Context, int, int, string, string, *int, *int64, *int64) (Page[Item], error) {
	return Page[Item]{}, nil
}
func (m *memStore) Create(_ context.Context, item Item) (int64, error) {
	m.created = item
	return 9, nil
}
func (m *memStore) Update(context.Context, Item) error  { return nil }
func (m *memStore) Delete(context.Context, int64) error { return nil }
