package area

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

func TestTreeMatchesFrozenAreaCSV(t *testing.T) {
	list := Tree()
	if len(list) != 34 {
		t.Fatalf("中国子节点应为 34，实际 %d", len(list))
	}
	if list[0].ID != 810000 || list[0].Name != "香港特别行政区" || len(list[0].Children) != 0 {
		t.Fatalf("首个省级节点与 Java 不一致：%+v", list[0])
	}
	raw, err := json.Marshal(list[0])
	if err != nil || !strings.Contains(string(raw), `"children":[]`) {
		t.Fatalf("叶子 children 应是空数组：%s %v", raw, err)
	}
	beijing := findNode(list, 110000)
	if beijing == nil || beijing.Name != "北京市" {
		t.Fatal("缺少北京市")
	}
	district := findNode(beijing.Children, 110101)
	if district == nil || district.Name != "东城区" {
		t.Fatal("缺少东城区")
	}
}

func TestFormatMatchesJava(t *testing.T) {
	cases := map[int]string{
		110101: "北京市 北京市 东城区",
		110000: "北京市",
		166:    "美国",
		0:      "全球",
	}
	for id, want := range cases {
		got, ok := Format(id)
		if !ok || got != want {
			t.Fatalf("format(%d)=%q ok=%v，Java 为 %q", id, got, ok, want)
		}
	}
	if _, ok := Format(99999999); ok {
		t.Fatal("未知编号不应格式化成功")
	}
}

func TestNameByIPMatchesJavaProbe(t *testing.T) {
	cases := map[string]string{
		"8.8.8.8":   "美国",
		"1.2.4.8":   "北京市 北京市",
		"127.0.0.1": "全球",
		"223.5.5.5": "浙江省 杭州市",
		" 8.8.8.8 ": "美国",
	}
	for ip, want := range cases {
		got, err := NameByIP(ip)
		if err != nil || got != want {
			t.Fatalf("ip %q => %q err=%v，Java 为 %q", ip, got, err, want)
		}
	}
	if _, err := NameByIP("not-an-ip"); err == nil {
		t.Fatal("非法 IP 应失败")
	}
}

func TestNameByIPConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := NameByIP("223.5.5.5")
			if err != nil || got != "浙江省 杭州市" {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestAreaHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Mount(r, testSessions())

	missing := call(r, http.MethodGet, "/admin-api/system/area/tree", "", "")
	if missing.code != 401 {
		t.Fatalf("未登录 code=%d", missing.code)
	}
	cross := call(r, http.MethodGet, "/admin-api/system/area/tree", "good", "2")
	if cross.code != 403 {
		t.Fatalf("跨租户 code=%d body=%s", cross.code, cross.raw)
	}
	tree := call(r, http.MethodGet, "/admin-api/system/area/tree", "good", "1")
	if tree.code != 0 || len(tree.list) != 34 {
		t.Fatalf("地区树 code=%d len=%d", tree.code, len(tree.list))
	}
	noIP := call(r, http.MethodGet, "/admin-api/system/area/get-by-ip", "good", "1")
	if noIP.code != 400 || noIP.msg != "请求参数缺失:ip" {
		t.Fatalf("缺 ip：code=%d msg=%s", noIP.code, noIP.msg)
	}
	badIP := call(r, http.MethodGet, "/admin-api/system/area/get-by-ip?ip=not-an-ip", "good", "1")
	if badIP.code != 500 || badIP.msg != "系统异常" {
		t.Fatalf("非法 ip：code=%d msg=%s", badIP.code, badIP.msg)
	}
	okIP := call(r, http.MethodGet, "/admin-api/system/area/get-by-ip?ip=223.5.5.5", "good", "1")
	if okIP.code != 0 || okIP.text != "浙江省 杭州市" {
		t.Fatalf("IP 查询：code=%d data=%q", okIP.code, okIP.text)
	}
	appTree := call(r, http.MethodGet, "/app-api/system/area/tree", "", "")
	if appTree.code != 0 || len(appTree.list) != 34 {
		t.Fatalf("应用端地区树 code=%d len=%d", appTree.code, len(appTree.list))
	}
}

type areaResult struct {
	code int
	msg  string
	text string
	list []Node
	raw  []byte
}

func call(r http.Handler, method, path, token, tenant string) areaResult {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenant != "" {
		req.Header.Set("tenant-id", tenant)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	out := areaResult{code: body.Code, msg: body.Msg, raw: w.Body.Bytes()}
	_ = json.Unmarshal(body.Data, &out.list)
	_ = json.Unmarshal(body.Data, &out.text)
	return out
}

func findNode(list []Node, id int) *Node {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
		if found := findNode(list[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}

type areaTokenStore struct{ auth.TokenStore }

func (areaTokenStore) FindAccess(_ context.Context, token string) (*auth.Token, error) {
	if token != "good" {
		return nil, nil
	}
	return &auth.Token{UserID: 7, UserType: 2, TenantID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (areaTokenStore) FindRefresh(context.Context, string) (*auth.Token, error) { return nil, nil }

type areaUserStore struct{ auth.UserStore }

func (areaUserStore) FindByID(_ context.Context, id int64) (*auth.User, error) {
	if id != 7 {
		return nil, nil
	}
	return &auth.User{ID: 7, TenantID: 1, Status: 0}, nil
}

type areaPermStore struct{ auth.PermissionStore }

func (areaPermStore) RolesByUser(context.Context, int64) ([]auth.Role, error) {
	return []auth.Role{{ID: 1, Code: "operator", Status: 0}}, nil
}
func (areaPermStore) MenusByRole(context.Context, []int64, bool) ([]auth.Menu, error) {
	return nil, nil
}

func testSessions() *auth.Service {
	return &auth.Service{Users: areaUserStore{}, Tokens: areaTokenStore{}, Permissions: areaPermStore{}}
}
