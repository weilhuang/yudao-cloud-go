package rolepostrpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/system/directory"
)

type fakeReader struct {
	posts   []Post
	roles   []Role
	postErr error
	roleErr error
	postIDs []int64
	roleIDs []int64
	calls   int
}

func (f *fakeReader) Posts(_ context.Context, _ int64, ids []int64) ([]Post, error) {
	f.calls++
	f.postIDs = append([]int64(nil), ids...)
	return selectPosts(f.posts, ids), f.postErr
}

func (f *fakeReader) Roles(_ context.Context, _ int64, ids []int64) ([]Role, error) {
	f.calls++
	f.roleIDs = append([]int64(nil), ids...)
	return selectRoles(f.roles, ids), f.roleErr
}

func selectPosts(posts []Post, ids []int64) []Post {
	result := make([]Post, 0)
	for _, post := range posts {
		for _, id := range ids {
			if post.ID == id {
				result = append(result, post)
				break
			}
		}
	}
	return result
}

func selectRoles(roles []Role, ids []int64) []Role {
	result := make([]Role, 0)
	for _, role := range roles {
		for _, id := range ids {
			if role.ID == id {
				result = append(result, role)
				break
			}
		}
	}
	return result
}

type testResult struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func TestRolePostRPCContract(t *testing.T) {
	fake := &fakeReader{
		posts: []Post{{ID: 1, Name: "开发", Code: "dev", Sort: 3, Status: 0},
			{ID: 2, Name: "测试", Code: "test", Sort: 4, Status: 1}},
		roles: []Role{{ID: 10, Name: "管理员", Code: "admin", Sort: 1, Status: 0},
			{ID: 11, Name: "只读", Code: "read", Sort: 2, Status: 1}},
	}
	r := gin.New()
	MountRPC(r, &Service{Reader: fake}, func(_ context.Context, tenantID int64, _ time.Time) error {
		if tenantID != 1 {
			return &directory.Error{Code: 1_002_015_001, Msg: "租户不存在"}
		}
		return nil
	})
	call := func(path, tenant string) testResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if tenant != "" {
			req.Header.Set("tenant-id", tenant)
		}
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s HTTP %d: %s", path, w.Code, w.Body.String())
		}
		var result testResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	// list 查询保留停用 DTO；只返回 Java 对外公开的五个字段。
	res := call("/rpc-api/system/post/list?ids=1,2", "1")
	var postList []map[string]any
	if res.Code != 0 || json.Unmarshal(res.Data, &postList) != nil || len(postList) != 2 ||
		len(postList[0]) != 5 || postList[1]["status"] != float64(1) ||
		!reflect.DeepEqual(fake.postIDs, []int64{1, 2}) {
		t.Fatalf("岗位列表与 PostRespDTO 不符：%+v, %s", res, res.Data)
	}
	res = call("/rpc-api/system/role/list?ids=10&ids=11", "1")
	var roleList []map[string]any
	if res.Code != 0 || json.Unmarshal(res.Data, &roleList) != nil || len(roleList) != 2 ||
		len(roleList[0]) != 5 || roleList[1]["status"] != float64(1) ||
		!reflect.DeepEqual(fake.roleIDs, []int64{10, 11}) {
		t.Fatalf("角色列表与 RoleRespDTO 不符：%+v, %s", res, res.Data)
	}
	res = call("/rpc-api/system/role/get?id=11", "1")
	var role Role
	if res.Code != 0 || json.Unmarshal(res.Data, &role) != nil || role.Name != "只读" || role.Status != 1 {
		t.Fatalf("停用角色详情丢失：%+v", res)
	}
	res = call("/rpc-api/system/role/get?id=999", "1")
	if res.Code != 0 || string(res.Data) != "null" {
		t.Fatalf("不存在角色应返回 data:null：%+v", res)
	}

	checks := []struct {
		path, tenant string
		code         int
		msg          string
		data         string
	}{
		{"/rpc-api/system/post/valid?ids=1", "1", 0, "", "true"},
		{"/rpc-api/system/post/valid?ids=", "1", 0, "", "true"},
		{"/rpc-api/system/post/valid?ids=2", "1", 1_002_005_001, "岗位(测试) 不处于开启状态，不允许选择", "null"},
		{"/rpc-api/system/post/valid?ids=999", "1", 1_002_005_000, "当前岗位不存在", "null"},
		{"/rpc-api/system/post/valid?ids=999,2", "1", 1_002_005_000, "当前岗位不存在", "null"},
		{"/rpc-api/system/role/valid?ids=10", "1", 0, "", "true"},
		{"/rpc-api/system/role/valid?ids=11", "1", 1_002_002_004, "名字为【只读】的角色已被禁用", "null"},
		{"/rpc-api/system/role/valid?ids=999", "1", 1_002_002_000, "角色不存在", "null"},
		{"/rpc-api/system/role/valid?ids=", "1", 0, "", "true"},
		{"/rpc-api/system/role/list?ids=", "1", 0, "", "[]"},
		{"/rpc-api/system/post/list?ids=", "1", 0, "", "[]"},
		{"/rpc-api/system/role/get", "1", 400, "请求参数不正确", "null"},
		{"/rpc-api/system/role/get?id=1&id=2", "1", 400, "请求参数不正确", "null"},
		{"/rpc-api/system/post/valid?ids=abc", "1", 400, "请求参数不正确", "null"},
		{"/rpc-api/system/post/list", "1", 400, "请求参数不正确", "null"},
		{"/rpc-api/system/role/get?id=10", "", 400, "请求的租户标识未传递，请进行排查", "null"},
		{"/rpc-api/system/role/get?id=10", "2", 1_002_015_001, "租户不存在", "null"},
	}
	for _, tc := range checks {
		res := call(tc.path, tc.tenant)
		if res.Code != tc.code || res.Msg != tc.msg || string(res.Data) != tc.data {
			t.Errorf("%s tenant=%q: got=%+v, want=(%d,%q,%s)", tc.path, tc.tenant, res, tc.code, tc.msg, tc.data)
		}
	}
}

func TestRolePostServiceEmptyAndDBError(t *testing.T) {
	fake := &fakeReader{postErr: errors.New("db unavailable")}
	svc := &Service{Reader: fake}
	posts, err := svc.PostList(context.Background(), 1, nil)
	if err != nil || posts == nil || len(posts) != 0 || fake.calls != 0 {
		t.Fatalf("空 ID 应短路数据库：%v, %v, %d", posts, err, fake.calls)
	}
	if err := svc.ValidPostList(context.Background(), 1, []int64{1}); err == nil || err.Error() != "db unavailable" {
		t.Fatalf("数据库故障不应伪装成业务缺失：%v", err)
	}
}
