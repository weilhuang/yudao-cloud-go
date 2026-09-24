package directory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDeptListHonorsDataScope(t *testing.T) {
	reader := deptListReader{list: []Dept{{ID: 10, Name: "本部"}, {ID: 20, Name: "外部门"}}}
	svc := &Service{Reader: reader}
	all, err := svc.DeptList(context.Background(), 1)
	if err != nil || len(all) != 2 {
		t.Fatalf("没有范围时应返回全部：%d %v", len(all), err)
	}
	scoped := withUserAccess(context.Background(), UserAccess{DeptIDs: []int64{10}, Self: true, UserID: 7})
	list, err := svc.DeptList(scoped, 1)
	if err != nil || len(list) != 1 || list[0].ID != 10 {
		t.Fatalf("本人范围不能放出部门表：%+v %v", list, err)
	}
	none := withUserAccess(context.Background(), UserAccess{Self: true, UserID: 7})
	list, err = svc.DeptList(none, 1)
	if err != nil || len(list) != 0 {
		t.Fatalf("只有本人时应为空：%+v %v", list, err)
	}
	got, err := svc.GetDept(scoped, 1, 20)
	if err != nil || got != nil {
		t.Fatalf("范围外详情应为空：%+v %v", got, err)
	}
}

type deptListReader struct {
	memReader
	list []Dept
}

func (d deptListReader) DeptList(context.Context, int64) ([]Dept, error) { return d.list, nil }
func (d deptListReader) DeptGet(_ context.Context, _, id int64) (*Dept, error) {
	for _, dept := range d.list {
		if dept.ID == id {
			copy := dept
			return &copy, nil
		}
	}
	return nil, nil
}

func TestRPCUserScopeFollowsLoginUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &scopeRPCReader{}
	r := gin.New()
	MountRPC(r, reader, func(context.Context, int64, time.Time) error { return nil }, func(_ context.Context, tenantID, userID int64) (UserAccess, error) {
		if tenantID != 1 || userID != 7 {
			t.Fatalf("范围参数：tenant=%d user=%d", tenantID, userID)
		}
		return UserAccess{DeptIDs: []int64{10}}, nil
	})
	header := url.QueryEscape(`{"id":7,"userType":2,"tenantId":1}`)
	call := func(path string, login bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("tenant-id", "1")
		if login {
			req.Header.Set("login-user", header)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatal(path, rec.Body.String())
		}
	}
	call("/rpc-api/system/user/get?id=9", true)
	if reader.apply {
		t.Fatal("按编号查询不应套数据范围")
	}
	call("/rpc-api/system/user/list-by-dept-id?deptIds=10", true)
	if !reader.apply || reader.access == nil || len(reader.access.DeptIDs) != 1 || reader.access.DeptIDs[0] != 10 {
		t.Fatalf("按部门查询应套范围：%+v %v", reader.access, reader.apply)
	}
	reader.apply = false
	call("/rpc-api/system/user/list-by-dept-id?deptIds=10", false)
	if reader.apply {
		t.Fatal("没有 login-user 时不套范围")
	}
	call("/rpc-api/system/dept/list?ids=10,20", true)
	if !reader.deptScoped {
		t.Fatal("部门 Feign 应看到数据范围")
	}
}

type scopeRPCReader struct {
	apply      bool
	deptScoped bool
	access     *UserAccess
}

func (s *scopeRPCReader) RPCUsers(ctx context.Context, _ int64, filter RPCUserFilter) ([]RPCUser, error) {
	s.apply = filter.ApplyAccess
	s.access = accessFrom(ctx)
	return []RPCUser{}, nil
}
func (s *scopeRPCReader) RPCDepts(ctx context.Context, _ int64, _ []int64) ([]RPCDept, error) {
	s.deptScoped = accessFrom(ctx) != nil
	return []RPCDept{}, nil
}
func (s *scopeRPCReader) RPCChildDepts(context.Context, int64, []int64) ([]RPCDept, error) {
	return nil, nil
}
func (s *scopeRPCReader) RPCDictDataList(context.Context, string) ([]RPCDictData, error) {
	return nil, nil
}
func (s *scopeRPCReader) RPCDictDataValues(context.Context, string, []string) ([]RPCDictData, error) {
	return nil, nil
}

func TestRPCRejectsBadLoginUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	MountRPC(r, &scopeRPCReader{}, func(context.Context, int64, time.Time) error { return nil }, nil)
	req := httptest.NewRequest(http.MethodGet, "/rpc-api/system/user/list-by-nickname?nickname=a", nil)
	req.Header.Set("tenant-id", "1")
	req.Header.Set("login-user", "not-json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "请求参数不正确") {
		t.Fatal(rec.Body.String())
	}
}
