//go:build integration

package demo02

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestDemo02CategoryTreeMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
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
	if _, err := db.Exec(demo02Schema); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db}
	root, err := svc.Create(ctx, 1, Save{Name: "土豆", ParentID: 0})
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.Create(ctx, 1, Save{Name: "薯条", ParentID: root})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := svc.Create(ctx, 1, Save{Name: "番茄", ParentID: child})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, 1, Save{Name: "薯条", ParentID: root}); err == nil || err.(*Error).Msg != "已经存在该名字的示例分类" {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, 1, Save{Name: "薯条", ParentID: 0}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Update(ctx, 1, Save{ID: root, Name: "土豆", ParentID: grand}); err == nil || err.(*Error).Msg != "不能设置自己的子示例分类为父示例分类" {
		t.Fatal(err)
	}
	if err := svc.Update(ctx, 1, Save{ID: child, Name: "薯条", ParentID: child}); err == nil || err.(*Error).Msg != "不能设置自己为父示例分类" {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1, root); err == nil || err.(*Error).Msg != "存在存在子示例分类，无法删除" {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1, grand); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, 2, child)
	if err != nil || got != nil {
		t.Fatalf("其他租户不应读到: %+v %v", got, err)
	}
	list, err := svc.List(ctx, 1, Query{Name: "薯"})
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
}

const demo02Schema = `
CREATE TABLE yudao_demo02_category (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  name varchar(100) NOT NULL DEFAULT '',
  parent_id bigint NOT NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL DEFAULT 0
);
`
