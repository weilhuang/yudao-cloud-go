package directory

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSaveTenantRejectsSystemTenant(t *testing.T) {
	svc := &Service{Tenants: &memTenants{tenant: &Tenant{ID: 1, PackageID: packageIDSystem}}}
	_, err := svc.SaveTenant(context.Background(), sampleTenant(1))
	assertCode(t, err, 1_002_015_003)
}

func TestSaveTenantCreatesAdminWithPackageMenus(t *testing.T) {
	store := &memTenants{pkg: &TenantPackage{ID: 10, Name: "基础", Status: 0, MenuIDs: []int64{1, 2}}}
	svc := &Service{Tenants: store, BcryptCost: 4}
	id, err := svc.SaveTenant(context.Background(), sampleTenant(0))
	if err != nil || id == 0 {
		t.Fatal(err, id)
	}
	if store.createdHash == "" || bcrypt.CompareHashAndPassword([]byte(store.createdHash), []byte("admin123")) != nil {
		t.Fatal("password hash missing")
	}
	if len(store.createdMenus) != 2 || store.createdMenus[0] != 1 {
		t.Fatal(store.createdMenus)
	}
}

func TestSaveTenantSyncsMenusWhenPackageChanges(t *testing.T) {
	store := &memTenants{
		tenant: &Tenant{ID: 8, Name: "旧", PackageID: 10, ContactName: "张三", ExpireTime: 1},
		pkg:    &TenantPackage{ID: 11, Name: "进阶", Status: 0, MenuIDs: []int64{9}},
	}
	svc := &Service{Tenants: store}
	item := sampleTenant(8)
	item.PackageID = 11
	if _, err := svc.SaveTenant(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !store.synced || len(store.syncedMenus) != 1 || store.syncedMenus[0] != 9 {
		t.Fatal(store.synced, store.syncedMenus)
	}
}

func TestDeletePackageBlockedWhenUsed(t *testing.T) {
	svc := &Service{Tenants: &memTenants{pkg: &TenantPackage{ID: 10, Name: "基础"}, used: true}}
	assertCode(t, svc.DeletePackage(context.Background(), 10), 1_002_016_001)
}

func TestChangeOwnPasswordRejectsWrongOldPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("old-pass"), 4)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Tenants: &memTenants{password: string(hash)}, BcryptCost: 4}
	assertCode(t, svc.ChangeOwnPassword(context.Background(), 1, "wrong", "new-pass"), 1_002_003_005)
}

func TestValidTenantExpired(t *testing.T) {
	svc := &Service{Tenants: &memTenants{tenant: &Tenant{ID: 2, Name: "演示", Status: 0, ExpireTime: time.Now().Add(-time.Hour).UnixMilli()}}}
	err := svc.ValidTenant(context.Background(), 2, time.Now())
	assertCode(t, err, 1_002_015_002)
	if !strings.Contains(err.Error(), "演示") {
		t.Fatal(err)
	}
}

func sampleTenant(id int64) Tenant {
	return Tenant{
		ID: id, Name: "演示租户", ContactName: "张三", Status: 0, PackageID: 10,
		ExpireTime: time.Now().Add(24 * time.Hour).UnixMilli(), AccountCount: 20,
		Username: "admin", Password: "admin123", Websites: []string{"demo.example.com"},
	}
}

func assertCode(t *testing.T, err error, code int) {
	t.Helper()
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != code {
		t.Fatal(err)
	}
}

type memTenants struct {
	tenant       *Tenant
	pkg          *TenantPackage
	used         bool
	nameTaken    bool
	website      string
	password     string
	createdHash  string
	createdMenus []int64
	synced       bool
	syncedMenus  []int64
	nextPassword string
}

func (m *memTenants) TenantByID(context.Context, int64) (*Tenant, error) { return m.tenant, nil }
func (m *memTenants) TenantByName(context.Context, string) (*Tenant, error) {
	return m.tenant, nil
}
func (m *memTenants) TenantByWebsite(context.Context, string) (*Tenant, error) {
	return m.tenant, nil
}
func (m *memTenants) TenantNameTaken(context.Context, string, int64) (bool, error) {
	return m.nameTaken, nil
}
func (m *memTenants) TenantWebsiteTaken(context.Context, []string, int64) (string, error) {
	return m.website, nil
}
func (m *memTenants) TenantPage(context.Context, int, int, string, string, *int) (Page[Tenant], error) {
	return Page[Tenant]{}, nil
}
func (m *memTenants) TenantExportRows(context.Context, TenantExportFilter, func(Tenant) error) error {
	return nil
}
func (m *memTenants) TenantSimple(context.Context) ([]Tenant, error) { return nil, nil }
func (m *memTenants) CreateTenant(_ context.Context, _ Tenant, passwordHash string, menuIDs []int64) (int64, error) {
	m.createdHash = passwordHash
	m.createdMenus = menuIDs
	return 21, nil
}
func (m *memTenants) UpdateTenant(_ context.Context, _ Tenant, menuIDs []int64, syncMenus bool) error {
	m.synced = syncMenus
	m.syncedMenus = menuIDs
	return nil
}
func (m *memTenants) DeleteTenant(context.Context, int64) error { return nil }
func (m *memTenants) PackageByID(context.Context, int64) (*TenantPackage, error) {
	return m.pkg, nil
}
func (m *memTenants) PackageNameTaken(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (m *memTenants) PackageUsed(context.Context, int64) (bool, error) { return m.used, nil }
func (m *memTenants) PackagePage(context.Context, int, int, string, *int) (Page[TenantPackage], error) {
	return Page[TenantPackage]{}, nil
}
func (m *memTenants) PackageSimple(context.Context) ([]TenantPackage, error) { return nil, nil }
func (m *memTenants) CreatePackage(context.Context, TenantPackage) (int64, error) {
	return 1, nil
}
func (m *memTenants) UpdatePackage(context.Context, TenantPackage) error { return nil }
func (m *memTenants) DeletePackage(context.Context, int64) error         { return nil }
func (m *memTenants) NoticePage(context.Context, int64, int, int, string, *int) (Page[Notice], error) {
	return Page[Notice]{}, nil
}
func (m *memTenants) NoticeByID(context.Context, int64, int64) (*Notice, error) {
	return &Notice{ID: 1, Title: "hello"}, nil
}
func (m *memTenants) CreateNotice(context.Context, int64, Notice) (int64, error) { return 1, nil }
func (m *memTenants) UpdateNotice(context.Context, int64, Notice) error          { return nil }
func (m *memTenants) DeleteNotice(context.Context, int64, int64) error           { return nil }
func (m *memTenants) ProfilePassword(context.Context, int64) (string, error) {
	return m.password, nil
}
func (m *memTenants) UpdateProfile(context.Context, int64, string, string, string, string, *int) error {
	return nil
}
func (m *memTenants) UpdateOAuthUser(context.Context, int64, int64, *string, *string, *string, *int) error {
	return nil
}
func (m *memTenants) UpdateOwnPassword(_ context.Context, _ int64, passwordHash string) error {
	m.nextPassword = passwordHash
	return nil
}
