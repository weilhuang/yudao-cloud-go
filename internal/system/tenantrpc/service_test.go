package tenantrpc

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeReader struct {
	ids     []int64
	tenant  *Tenant
	listErr error
	getErr  error
	gotID   int64
}

func (f *fakeReader) TenantIDs(context.Context) ([]int64, error) { return f.ids, f.listErr }
func (f *fakeReader) TenantByID(_ context.Context, id int64) (*Tenant, error) {
	f.gotID = id
	return f.tenant, f.getErr
}

func TestTenantIDListPreservesAllUndeletedIDsAndEmptyArray(t *testing.T) {
	reader := &fakeReader{ids: []int64{1, 2, 3}}
	got, err := (&Service{Reader: reader}).TenantIDList(context.Background())
	if err != nil || !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("租户列表不应按状态或到期日过滤：%v, %v", got, err)
	}
	reader.ids = nil
	got, err = (&Service{Reader: reader}).TenantIDList(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("空结果应编码为 []：%v, %v", got, err)
	}
	reader.listErr = errors.New("database offline")
	if _, err := (&Service{Reader: reader}).TenantIDList(context.Background()); err != reader.listErr {
		t.Fatalf("数据库错误不能被伪装为空列表：%v", err)
	}
}

func TestValidTenantMatchesJavaOrderAndExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.Local)
	reader := &fakeReader{}
	svc := &Service{Reader: reader}
	assertErrorCode(t, svc.ValidTenant(context.Background(), 42, now), 1_002_015_000, "租户不存在")
	if reader.gotID != 42 {
		t.Fatalf("未查询请求编号：%d", reader.gotID)
	}
	reader.tenant = &Tenant{ID: 42, Name: "测试", Status: 1, ExpireTime: now.Add(-time.Second)}
	assertErrorCode(t, svc.ValidTenant(context.Background(), 42, now), 1_002_015_001, "名字为【测试】的租户已被禁用")
	reader.tenant.Status = 0
	assertErrorCode(t, svc.ValidTenant(context.Background(), 42, now), 1_002_015_002, "名字为【测试】的租户已过期")
	reader.tenant.ExpireTime = now
	if err := svc.ValidTenant(context.Background(), 42, now); err != nil {
		t.Fatalf("Java isExpired 使用 now.isAfter，等于到期时间仍有效：%v", err)
	}
	reader.tenant.ExpireTime = now.Add(time.Second)
	if err := svc.ValidTenant(context.Background(), 42, now); err != nil {
		t.Fatalf("有效租户误判：%v", err)
	}
	reader.getErr = errors.New("database offline")
	if err := svc.ValidTenant(context.Background(), 42, now); err != reader.getErr {
		t.Fatalf("数据库错误不能伪装成租户不存在：%v", err)
	}
}

func assertErrorCode(t *testing.T, err error, code int, msg string) {
	t.Helper()
	actual, ok := err.(*Error)
	if !ok || actual.Code != code || actual.Msg != msg {
		t.Fatalf("got=%v, want=(%d,%q)", err, code, msg)
	}
}
