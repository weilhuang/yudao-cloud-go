package file

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestUploadUsesMasterS3NotLocal(t *testing.T) {
	raw := mustJSON(ClientConfig{Endpoint: "s3-cn-south-1.qiniucs.com", Domain: "http://cdn.example.com", Bucket: "demo", AccessKey: "ak", AccessSecret: "sk", EnablePublicAccess: boolPtr(true)})
	store := &memStore{master: &Config{ID: 22, Name: "七牛", Storage: storageS3, Master: true, Config: raw}}
	backend := &memBackend{}
	svc := &Service{Store: store, Now: fixedNow, Open: func(cfg Config) (ObjectClient, error) {
		if cfg.Storage != storageS3 || cfg.ID != 22 {
			t.Fatalf("opened storage %d id %d", cfg.Storage, cfg.ID)
		}
		return backend, nil
	}}
	url, err := svc.Upload(context.Background(), []byte("png"), "a.png", "", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://cdn.example.com/saved" || backend.path != "20260922/a.png" || store.saved.ConfigID != 22 {
		t.Fatalf("url %s path %s saved %+v", url, backend.path, store.saved)
	}
}

func TestDeleteMasterConfigRejected(t *testing.T) {
	svc := &Service{Store: &memStore{byID: &Config{ID: 22, Master: true}}}
	err := svc.DeleteConfig(context.Background(), 22)
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_001_006_001 {
		t.Fatal(err)
	}
}

func TestUploadRejectsEmpty(t *testing.T) {
	svc := &Service{Store: &memStore{}}
	_, err := svc.Upload(context.Background(), nil, "a.png", "", "")
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_001_003_002 {
		t.Fatal(err)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
}

func boolPtr(v bool) *bool { return &v }

type memBackend struct {
	path    string
	content []byte
	gotGet  string
}

func (m *memBackend) Upload(_ context.Context, _ []byte, objectPath, _ string) (string, error) {
	m.path = objectPath
	return "http://cdn.example.com/saved", nil
}
func (m *memBackend) Delete(context.Context, string) error { return nil }
func (m *memBackend) Get(_ context.Context, objectPath string) ([]byte, error) {
	m.gotGet = objectPath
	if m.content != nil {
		return m.content, nil
	}
	return []byte("ok"), nil
}
func (m *memBackend) PresignPut(context.Context, string) (string, error) { return "", nil }
func (m *memBackend) PresignGet(context.Context, string, int) (string, error) {
	return "", nil
}

type memStore struct {
	master *Config
	byID   *Config
	byPath *Item
	saved  Item
}

func (m *memStore) Master(context.Context) (*Config, error) { return m.master, nil }
func (m *memStore) ConfigByID(context.Context, int64) (*Config, error) {
	return m.byID, nil
}
func (m *memStore) ConfigPage(context.Context, int, int, string, *int) (Page[Config], error) {
	return Page[Config]{}, nil
}
func (m *memStore) CreateConfig(context.Context, Config) (int64, error) { return 1, nil }
func (m *memStore) UpdateConfig(context.Context, Config) error          { return nil }
func (m *memStore) UpdateMaster(context.Context, int64) error           { return nil }
func (m *memStore) DeleteConfig(context.Context, int64) error           { return nil }
func (m *memStore) InsertFile(_ context.Context, item Item) (int64, error) {
	m.saved = item
	return 1, nil
}
func (m *memStore) FilePage(context.Context, int, int, string, string) (Page[Item], error) {
	return Page[Item]{}, nil
}
func (m *memStore) FileByID(context.Context, int64) (*Item, error) { return nil, nil }
func (m *memStore) FileByPath(context.Context, int64, string) (*Item, error) {
	return m.byPath, nil
}
func (m *memStore) DeleteFile(context.Context, int64) error { return nil }

func TestConfigJSONKeepsClass(t *testing.T) {
	raw := json.RawMessage(`{"@class":"s3","endpoint":"s3-cn-south-1.qiniucs.com","domain":"http://cdn.example.com","bucket":"b","accessKey":"k","accessSecret":"s"}`)
	cfg, err := parseClient(raw)
	if err != nil || cfg.Bucket != "b" || cfg.Domain == "" {
		t.Fatal(cfg, err)
	}
}
