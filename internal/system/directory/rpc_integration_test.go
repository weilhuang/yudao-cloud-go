//go:build integration

package directory

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

// TestSystemRPC 使用真实 MySQL 和 HTTP 路由检验两个 Java Feign 接口的租户与数据语义。
func TestSystemRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("rpc"),
		mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })
	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, systemRPCSchema); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	store := &MySQL{DB: db}
	MountRPC(r, store, (&Service{Tenants: store}).ValidTenant, nil)

	call := func(path, tenant string) rpcTestResult {
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
		var result rpcTestResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	assertUserIDs := func(path, tenant string, want ...int64) {
		t.Helper()
		res := call(path, tenant)
		if res.Code != 0 {
			t.Fatalf("%s: code=%d msg=%s", path, res.Code, res.Msg)
		}
		var list []RPCUser
		if err := json.Unmarshal(res.Data, &list); err != nil {
			t.Fatal(err)
		}
		if len(list) != len(want) {
			t.Fatalf("%s: users=%+v want=%v", path, list, want)
		}
		for i := range want {
			if list[i].ID != want[i] {
				t.Fatalf("%s: users=%+v want=%v", path, list, want)
			}
		}
	}
	assertDeptIDs := func(path, tenant string, want ...int64) {
		t.Helper()
		res := call(path, tenant)
		if res.Code != 0 {
			t.Fatalf("%s: code=%d msg=%s", path, res.Code, res.Msg)
		}
		var list []RPCDept
		if err := json.Unmarshal(res.Data, &list); err != nil {
			t.Fatal(err)
		}
		if len(list) != len(want) {
			t.Fatalf("%s: depts=%+v want=%v", path, list, want)
		}
		for i := range want {
			if list[i].ID != want[i] {
				t.Fatalf("%s: depts=%+v want=%v", path, list, want)
			}
		}
	}

	// 单项查询缺失时返回 data:null；列表不含软删记录、其他租户记录，却保留禁用记录。
	user := call("/rpc-api/system/user/get?id=2", "1")
	var got RPCUser
	if user.Code != 0 || json.Unmarshal(user.Data, &got) != nil || got.ID != 2 ||
		got.DeptID == nil || *got.DeptID != 11 || len(got.PostIDs) != 1 || got.PostIDs[0] != 7 || got.Mobile != "222" {
		t.Fatalf("用户 DTO 不匹配：%+v, %s", got, user.Data)
	}
	if res := call("/rpc-api/system/user/get?id=5", "1"); res.Code != 0 || string(res.Data) != "null" {
		t.Fatalf("跨租户用户可见：%+v", res)
	}
	if res := call("/rpc-api/system/user/get-by-mobile?mobile=222", "2"); res.Code != 0 || json.Unmarshal(res.Data, &got) != nil || got.ID != 5 {
		t.Fatalf("相同手机号没有按租户查找：%+v", res)
	}
	if res := call("/rpc-api/system/user/get-by-mobile?mobile=333", "2"); res.Code != 0 || string(res.Data) != "null" {
		t.Fatalf("跨租户手机号可见：%+v", res)
	}
	if res := call("/rpc-api/system/user/get-by-mobile?mobile=222", "1"); res.Code != 0 || json.Unmarshal(res.Data, &got) != nil || got.ID != 2 {
		t.Fatalf("手机号查询错误：%+v", res)
	}
	assertUserIDs("/rpc-api/system/user/list?ids=2,3,4,5", "1", 2, 3)
	assertUserIDs("/rpc-api/system/user/list?ids=2&ids=3", "1", 2, 3)
	assertUserIDs("/rpc-api/system/user/list-by-dept-id?deptIds=11,12", "1", 2, 3)
	assertUserIDs("/rpc-api/system/user/list-by-post-id?postIds=7", "1", 2)
	assertUserIDs("/rpc-api/system/user/list-by-nickname?nickname=用户", "1", 2, 3, 6)
	assertUserIDs("/rpc-api/system/user/list-by-subordinate?id=1", "1", 2, 3, 6)
	assertUserIDs("/rpc-api/system/user/list-by-subordinate?id=2", "1")
	assertUserIDs("/rpc-api/system/user/list-by-subordinate?id=5", "1")

	assertDeptIDs("/rpc-api/system/dept/list?ids=10,11,13,20", "1", 10, 11)
	assertDeptIDs("/rpc-api/system/dept/list-child?id=10", "1", 11, 14, 12)
	assertDeptIDs("/rpc-api/system/dept/list-child-by-ids?ids=10,14", "1", 11, 12)
	assertDeptIDs("/rpc-api/system/dept/list-parent?id=12", "1", 11, 10)
	if res := call("/rpc-api/system/dept/get?id=20", "1"); res.Code != 0 || string(res.Data) != "null" {
		t.Fatalf("跨租户部门可见：%+v", res)
	}
	if res := call("/rpc-api/system/dept/get?id=11", "1"); res.Code != 0 || json.Unmarshal(res.Data, new(RPCDept)) != nil {
		t.Fatalf("部门 get 出错：%+v", res)
	}

	// valid 与 Java 服务层一致：空集为 true，缺失与禁用返回各自的业务码。
	checks := []struct {
		path string
		code int
	}{
		{"/rpc-api/system/user/valid?ids=2", 0},
		{"/rpc-api/system/user/valid?ids=", 0},
		{"/rpc-api/system/user/valid?ids=5", 1_002_003_003},
		{"/rpc-api/system/user/valid?ids=3", 1_002_003_006},
		{"/rpc-api/system/dept/valid?ids=10,11", 0},
		{"/rpc-api/system/dept/valid?ids=", 0},
		{"/rpc-api/system/dept/valid?ids=20", 1_002_004_002},
		{"/rpc-api/system/dept/valid?ids=14", 1_002_004_006},
	}
	for _, tc := range checks {
		res := call(tc.path, "1")
		if res.Code != tc.code {
			t.Fatalf("%s: got=%d want=%d msg=%s", tc.path, res.Code, tc.code, res.Msg)
		}
	}
	if res := call("/rpc-api/system/user/get?id=2", ""); res.Code != 400 {
		t.Fatalf("没有 tenant-id 仍可读：%+v", res)
	}
	for _, tc := range []struct {
		tenant string
		code   int
	}{{"3", 1_002_015_001}, {"4", 1_002_015_002}, {"5", 1_002_015_000}} {
		if res := call("/rpc-api/system/user/get?id=2", tc.tenant); res.Code != tc.code {
			t.Fatalf("无效租户 %s 没有被拒绝：%+v", tc.tenant, res)
		}
	}
	if res := call("/rpc-api/system/user/list?ids=2,x", "1"); res.Code != 400 {
		t.Fatalf("无效 ids 未拒绝：%+v", res)
	}
}

type rpcTestResult struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

const systemRPCSchema = `
CREATE TABLE system_dept (
 id bigint PRIMARY KEY, name varchar(30) NOT NULL, parent_id bigint NOT NULL,
 leader_user_id bigint NULL, status tinyint NOT NULL, deleted bit(1) NOT NULL DEFAULT 0, tenant_id bigint NOT NULL
);
CREATE TABLE system_users (
 id bigint PRIMARY KEY, nickname varchar(30) NOT NULL, status tinyint NOT NULL,
 dept_id bigint NULL, post_ids varchar(255) NULL, mobile varchar(30) NULL, email varchar(50) NULL,
 sex tinyint NULL, avatar varchar(512) NULL, deleted bit(1) NOT NULL DEFAULT 0, tenant_id bigint NOT NULL
);
CREATE TABLE system_user_post (
 id bigint PRIMARY KEY, user_id bigint NOT NULL, post_id bigint NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0, tenant_id bigint NOT NULL
);
CREATE TABLE system_tenant (
 id bigint PRIMARY KEY, name varchar(30) NOT NULL, contact_name varchar(30) NULL,
 contact_mobile varchar(30) NULL, status tinyint NOT NULL, websites varchar(255) NULL,
 package_id bigint NOT NULL, expire_time datetime NULL, account_count int NOT NULL,
 create_time datetime NULL, deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_tenant VALUES
 (1,'租户一','',NULL,0,'[]',0,NOW()+INTERVAL 1 DAY,10,NOW(),0),
 (2,'租户二','',NULL,0,'[]',0,NOW()+INTERVAL 1 DAY,10,NOW(),0),
 (3,'禁用租户','',NULL,1,'[]',0,NOW()+INTERVAL 1 DAY,10,NOW(),0),
 (4,'过期租户','',NULL,0,'[]',0,NOW()-INTERVAL 1 DAY,10,NOW(),0);
INSERT INTO system_dept VALUES
 (10,'总部门',0,1,0,0,1),(11,'子部门',10,NULL,0,0,1),(12,'孙部门',11,NULL,0,0,1),
 (13,'已删除',10,NULL,0,1,1),(14,'禁用部门',10,NULL,1,0,1),
 (20,'其他租户',10,5,0,0,2);
INSERT INTO system_users VALUES
 (1,'负责人',0,10,'[7]','111','',NULL,'',0,1),
 (2,'子用户',0,11,'[7]','222','sub@test',1,'',0,1),
 (3,'孙用户',1,12,'[8]','333','',NULL,'',0,1),
 (4,'已删除用户',0,12,NULL,'444','',NULL,'',1,1),
 (5,'其他租户',0,20,'[7]','222','',NULL,'',0,2),
 (6,'侧用户',0,14,'[]','666','',NULL,'',0,1);
INSERT INTO system_user_post VALUES (1,2,7,0,1),(2,5,7,0,2),(3,6,7,1,1),(4,3,8,0,1);
`
