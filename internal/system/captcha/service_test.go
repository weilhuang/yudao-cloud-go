package captcha

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSliderCheckAndVerifyOnce(t *testing.T) {
	cache := &memCache{data: map[string]string{}}
	svc := &Service{Cache: cache}
	got, err := svc.Get(context.Background())
	if err != nil || got.Token == "" || got.SecretKey == "" || got.OriginalImageBase64 == "" {
		t.Fatal(err, got)
	}
	raw, _ := cache.data["captcha:"+got.Token]
	gap := 0
	for i := 0; i < len(raw) && raw[i] != '|'; i++ {
		gap = gap*10 + int(raw[i]-'0')
	}
	point, _ := json.Marshal(map[string]int{"x": gap, "y": 5})
	encrypted, err := aesEncrypt(string(point), got.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := svc.Check(context.Background(), got.Token, encrypted)
	if err != nil || verification == "" {
		t.Fatal(err, verification)
	}
	if err := svc.Verify(context.Background(), verification); err != nil {
		t.Fatal(err)
	}
	if err := svc.Verify(context.Background(), verification); err == nil {
		t.Fatal("verification should be one-time")
	}
}

func TestCheckRejectsWrongX(t *testing.T) {
	cache := &memCache{data: map[string]string{}}
	svc := &Service{Cache: cache, Now: func() time.Time { return time.Now() }}
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	point, _ := json.Marshal(map[string]int{"x": 1, "y": 5})
	encrypted, _ := aesEncrypt(string(point), got.SecretKey)
	if _, err := svc.Check(context.Background(), got.Token, encrypted); err == nil {
		t.Fatal("expected mismatch")
	}
}

type memCache struct {
	data map[string]string
}

func (m *memCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.data[key] = value
	return nil
}

func (m *memCache) GetDel(_ context.Context, key string) (string, error) {
	value := m.data[key]
	delete(m.data, key)
	return value, nil
}
