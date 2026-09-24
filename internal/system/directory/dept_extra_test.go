package directory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

type deptTokenStoreFake struct{ auth.TokenStore }

func (deptTokenStoreFake) FindAccess(_ context.Context, token string) (*auth.Token, error) {
	if token != "good" {
		return nil, nil
	}
	return &auth.Token{UserID: 7, UserType: 2, TenantID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (deptTokenStoreFake) FindRefresh(context.Context, string) (*auth.Token, error) {
	return nil, nil
}

type deptUserStoreFake struct{ auth.UserStore }

func (deptUserStoreFake) FindByID(_ context.Context, id int64) (*auth.User, error) {
	if id != 7 {
		return nil, nil
	}
	return &auth.User{ID: 7, TenantID: 1, Status: 0}, nil
}

type deptPermissionFake struct {
	auth.PermissionStore
	allowed []string
}

func (deptPermissionFake) RolesByUser(context.Context, int64) ([]auth.Role, error) {
	return []auth.Role{{ID: 9, Code: "operator", Status: 0}}, nil
}
func (f deptPermissionFake) MenusByRole(context.Context, []int64, bool) ([]auth.Menu, error) {
	menus := make([]auth.Menu, 0, len(f.allowed))
	for i, perm := range f.allowed {
		menus = append(menus, auth.Menu{ID: int64(i + 1), Permission: perm, Type: 3, Status: 0})
	}
	return menus, nil
}

type deptReaderFake struct {
	Reader
	getCalls int
	tenants  []int64
	dept     *Dept
}

func (f *deptReaderFake) DeptGet(_ context.Context, tenantID, id int64) (*Dept, error) {
	f.getCalls++
	f.tenants = append(f.tenants, tenantID)
	if f.dept != nil && f.dept.ID == id && tenantID == 1 {
		return f.dept, nil
	}
	return nil, nil
}

type deptWriterFake struct {
	DeptWriter
	deleteIDs   []int64
	deleteLists [][]int64
	tenants     []int64
}

func (f *deptWriterFake) DeleteDept(_ context.Context, tenantID, id int64) error {
	f.tenants = append(f.tenants, tenantID)
	f.deleteIDs = append(f.deleteIDs, id)
	return nil
}
func (f *deptWriterFake) DeleteDeptList(_ context.Context, tenantID int64, ids []int64) error {
	f.tenants = append(f.tenants, tenantID)
	f.deleteLists = append(f.deleteLists, append([]int64(nil), ids...))
	return nil
}

func deptExtraRouter(perms []string, reader *deptReaderFake, writer *deptWriterFake) *gin.Engine {
	sessions := &auth.Service{Users: deptUserStoreFake{}, Tokens: deptTokenStoreFake{}, Permissions: deptPermissionFake{allowed: perms}}
	r := gin.New()
	MountDeptExtras(r, sessions, &Service{
		Reader: reader, Depts: writer,
		DeptScope: func(context.Context, int64, int64) (UserAccess, error) { return UserAccess{All: true}, nil },
	})
	return r
}

func deptExtraCall(r http.Handler, method, path, token, tenant string) (int, json.RawMessage) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenant != "" {
		req.Header.Set("tenant-id", tenant)
	}
	r.ServeHTTP(w, req)
	var result struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		return -1, json.RawMessage(w.Body.Bytes())
	}
	return result.Code, result.Data
}

func TestDeptExtraHTTPContractAndPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &deptReaderFake{dept: &Dept{ID: 10, Name: "研发", ParentID: 1, Sort: 2, Status: 0, CreateTime: 1700000000000}}
	writer := &deptWriterFake{}
	r := deptExtraRouter([]string{"system:dept:query", "system:dept:delete"}, reader, writer)
	get := "/admin-api/system/dept/get?id=10"
	code, data := deptExtraCall(r, http.MethodGet, get, "good", "1")
	if code != 0 {
		t.Fatalf("合法查询失败：code=%d data=%s", code, data)
	}
	var dept Dept
	if err := json.Unmarshal(data, &dept); err != nil || dept.ID != 10 || dept.CreateTime != 1700000000000 {
		t.Fatalf("详情 DTO 不正确：%s %v", data, err)
	}
	code, data = deptExtraCall(r, http.MethodGet, "/admin-api/system/dept/get?id=99", "good", "1")
	if code != 0 || string(data) != "null" {
		t.Fatalf("不存在部门应返回 data:null：code=%d data=%s", code, data)
	}
	for _, path := range []string{"/admin-api/system/dept/get", "/admin-api/system/dept/get?id=abc", "/admin-api/system/dept/delete?id=1,2"} {
		method := http.MethodGet
		if path == "/admin-api/system/dept/delete?id=1,2" {
			method = http.MethodDelete
		}
		code, _ := deptExtraCall(r, method, path, "good", "1")
		if code != 400 {
			t.Fatalf("非法 ID 未被拒绝：%s code=%d", path, code)
		}
	}
	code, _ = deptExtraCall(r, http.MethodDelete, "/admin-api/system/dept/delete?id=10", "good", "1")
	if code != 0 || !reflect.DeepEqual(writer.deleteIDs, []int64{10}) {
		t.Fatalf("单删未调用存储：code=%d ids=%v", code, writer.deleteIDs)
	}
	code, _ = deptExtraCall(r, http.MethodDelete, "/admin-api/system/dept/delete-list?ids=10,11&ids=12", "good", "1")
	if code != 0 || len(writer.deleteLists) != 1 || !reflect.DeepEqual(writer.deleteLists[0], []int64{10, 11, 12}) {
		t.Fatalf("批删参数错误：code=%d ids=%v", code, writer.deleteLists)
	}
	code, _ = deptExtraCall(r, http.MethodDelete, "/admin-api/system/dept/delete-list", "good", "1")
	if code != 400 || len(writer.deleteLists) != 1 {
		t.Fatalf("缺失批删 ids 不应写库：code=%d ids=%v", code, writer.deleteLists)
	}
	if !reflect.DeepEqual(reader.tenants, []int64{1, 1}) || !reflect.DeepEqual(writer.tenants, []int64{1, 1}) {
		t.Fatalf("请求租户未向存储层透传：read=%v write=%v", reader.tenants, writer.tenants)
	}

	noPermReader, noPermWriter := &deptReaderFake{}, &deptWriterFake{}
	noPerm := deptExtraRouter(nil, noPermReader, noPermWriter)
	for _, test := range []struct {
		method, path, token, tenant string
		want                        int
	}{
		{http.MethodGet, get, "", "1", 401},
		{http.MethodGet, get, "good", "2", 403},
		{http.MethodGet, get, "good", "1", 403},
		{http.MethodDelete, "/admin-api/system/dept/delete?id=10", "good", "1", 403},
		{http.MethodDelete, "/admin-api/system/dept/delete-list?ids=10", "good", "1", 403},
	} {
		code, _ := deptExtraCall(noPerm, test.method, test.path, test.token, test.tenant)
		if code != test.want {
			t.Fatalf("鉴权错误：%s %s token=%q tenant=%q code=%d want=%d", test.method, test.path, test.token, test.tenant, code, test.want)
		}
	}
	if noPermReader.getCalls != 0 || len(noPermWriter.deleteIDs) != 0 || len(noPermWriter.deleteLists) != 0 {
		t.Fatal("未授权请求访问了存储层")
	}
}
