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

// TestDictRPCMySQL 用真实 MySQL 与 HTTP 路由证明逻辑删除、排序和全局字典语义。
func TestDictRPCMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("dict_rpc"),
		mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dsn, err := container.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, dictRPCSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	r := gin.New()
	MountRPC(r, store, (&Service{Tenants: store}).ValidTenant, nil)
	call := func(path, tenant string) dictRPCResult {
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
		var res dictRPCResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}
	list := func(tenant string) []RPCDictData {
		t.Helper()
		res := call("/rpc-api/system/dict-data/list?dictType=sys_sex", tenant)
		if res.Code != 0 {
			t.Fatalf("租户 %s 字典列表错误：%+v", tenant, res)
		}
		var items []RPCDictData
		if err := json.Unmarshal(res.Data, &items); err != nil {
			t.Fatal(err)
		}
		return items
	}
	for _, tenant := range []string{"1", "2"} {
		items := list(tenant)
		if len(items) != 3 || items[0].Value != "1" || items[1].Value != "4" ||
			items[2].Value != "2" || items[2].Status != 1 {
			t.Fatalf("租户 %s 的全局字典排序/禁用/软删语义错误：%+v", tenant, items)
		}
	}
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=1,4", 0},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=", 0},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=3", 1_002_007_001},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=2", 1_002_007_002},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=9", 1_002_007_001},
	} {
		if res := call(tc.path, "1"); res.Code != tc.code {
			t.Fatalf("%s: code=%d want=%d msg=%s", tc.path, res.Code, tc.code, res.Msg)
		}
	}
	if res := call("/rpc-api/system/dict-data/list?dictType=sys_sex", "3"); res.Code != 1_002_015_001 {
		t.Fatalf("禁用租户通过了 RPC：%+v", res)
	}
	// 同一连接池更新后再查，确认本实现没有过期的进程内缓存。
	if _, err := db.ExecContext(ctx, `UPDATE system_dict_data SET deleted=1 WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if items := list("2"); len(items) != 2 || items[0].Value != "1" || items[1].Value != "2" {
		t.Fatalf("软删后列表未更新：%+v", items)
	}
	if res := call("/rpc-api/system/dict-data/valid?dictType=sys_sex&values=4", "2"); res.Code != 1_002_007_001 {
		t.Fatalf("软删后仍有效：%+v", res)
	}
}

const dictRPCSchema = `
CREATE TABLE system_tenant (
 id bigint PRIMARY KEY, name varchar(30) NOT NULL, contact_name varchar(30) NULL,
 contact_mobile varchar(30) NULL, status tinyint NOT NULL, websites varchar(255) NULL,
 package_id bigint NOT NULL, expire_time datetime NULL, account_count int NOT NULL,
 create_time datetime NULL, deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_dict_data (
 id bigint PRIMARY KEY, sort int NOT NULL, label varchar(100) NOT NULL,
 value varchar(100) NOT NULL, dict_type varchar(100) NOT NULL,
 status tinyint NOT NULL, deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_tenant VALUES
 (1,'租户一','',NULL,0,'[]',0,NOW()+INTERVAL 1 DAY,10,NOW(),0),
 (2,'租户二','',NULL,0,'[]',0,NOW()+INTERVAL 1 DAY,10,NOW(),0),
 (3,'禁用租户','',NULL,1,'[]',0,NOW()+INTERVAL 1 DAY,10,NOW(),0);
INSERT INTO system_dict_data VALUES
 (1,10,'男','1','sys_sex',0,0),
 (2,30,'女','2','sys_sex',1,0),
 (3,5,'已删除','3','sys_sex',0,1),
 (4,20,'其他','4','sys_sex',0,0),
 (5,1,'其他类型','1','other',0,0);
`
