package apilog

import (
	"context"
	"testing"
)

func TestMarkErrorRejectsProcessed(t *testing.T) {
	store := &memStore{errLog: &ErrorLog{ID: 1, ProcessStatus: 1}}
	err := MarkError(context.Background(), store, 1, 1, 9, 2)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_001_002_001 {
		t.Fatal(err)
	}
}

func TestMarkErrorMissing(t *testing.T) {
	err := MarkError(context.Background(), &memStore{}, 1, 1, 9, 1)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_001_002_000 {
		t.Fatal(err)
	}
}

func TestMarkErrorUpdatesInit(t *testing.T) {
	store := &memStore{errLog: &ErrorLog{ID: 3, ProcessStatus: 0}}
	if err := MarkError(context.Background(), store, 1, 3, 9, 1); err != nil {
		t.Fatal(err)
	}
	if store.status != 1 || store.userID != 9 {
		t.Fatal(store.status, store.userID)
	}
}

type memStore struct {
	errLog   *ErrorLog
	status   int
	userID   int64
	tenantID int64
	access   AccessLog
	errItem  ErrorLog
}

func (m *memStore) CreateAccess(_ context.Context, tenantID int64, item AccessLog) error {
	m.tenantID = tenantID
	m.access = item
	return nil
}
func (m *memStore) AccessPage(context.Context, int64, AccessQuery) (Page[AccessLog], error) {
	return Page[AccessLog]{}, nil
}
func (m *memStore) AccessByID(context.Context, int64, int64) (*AccessLog, error) { return nil, nil }
func (m *memStore) CreateError(_ context.Context, _ int64, item ErrorLog) error {
	m.errItem = item
	return nil
}
func (m *memStore) ErrorPage(context.Context, int64, ErrorQuery) (Page[ErrorLog], error) {
	return Page[ErrorLog]{}, nil
}
func (m *memStore) ErrorByID(context.Context, int64, int64) (*ErrorLog, error) { return m.errLog, nil }
func (m *memStore) UpdateErrorStatus(_ context.Context, _, userID int64, status int) error {
	m.status = status
	m.userID = userID
	return nil
}
