//go:build integration

package demo01

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestDemo01ContactMySQL(t *testing.T) {
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
	if _, err := db.Exec(demo01Schema); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db}
	avatar := "http://avatar"
	id, err := svc.Create(ctx, 1, Save{Name: "张三", Sex: 1, Birthday: 1699286400000, Description: "简介", Avatar: &avatar})
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	other, err := svc.Create(ctx, 2, Save{Name: "张三", Sex: 1, Birthday: 1699286400000, Description: "其他租户"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.Page(ctx, 1, Query{PageNo: 1, PageSize: 10, Name: "张", Sex: intPtr(1)})
	if err != nil || page.Total != 1 || page.List[0].ID != id {
		t.Fatalf("%+v %v", page, err)
	}
	got, err := svc.Get(ctx, 2, id)
	if err != nil || got != nil {
		t.Fatalf("其他租户不应读到: %+v %v", got, err)
	}
	if err := svc.Update(ctx, 1, Save{ID: id, Name: "李四", Sex: 2, Birthday: 1699286400000, Description: "改过"}); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Get(ctx, 1, id)
	if err != nil || got.Name != "李四" || got.Avatar != "http://avatar" || got.Sex != 2 {
		t.Fatalf("未提交的头像被清掉或更新失败: %+v %v", got, err)
	}
	labels, err := svc.sexLabels(ctx)
	if err != nil || labels["2"] != "女" {
		t.Fatal(labels, err)
	}
	if err := svc.DeleteList(ctx, 1, []int64{id, other}); err == nil || err.(*Error).Msg != "示例联系人不存在" {
		t.Fatal(err)
	}
	if err := svc.DeleteList(ctx, 1, []int64{id, id}); err == nil {
		t.Fatal("重复编号应拒绝")
	}
	still, err := svc.Get(ctx, 1, id)
	if err != nil || still == nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1, id); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1, id); err == nil || err.(*Error).Code != codeNotExists {
		t.Fatal(err)
	}
}

func intPtr(v int) *int { return &v }

const demo01Schema = `
CREATE TABLE yudao_demo01_contact (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  name varchar(100) NOT NULL DEFAULT '',
  sex tinyint NOT NULL,
  birthday datetime NOT NULL,
  description varchar(255) NOT NULL,
  avatar varchar(512) NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL DEFAULT 0
);
CREATE TABLE system_dict_data (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  sort int NOT NULL DEFAULT 0,
  label varchar(100) NOT NULL,
  value varchar(100) NOT NULL,
  dict_type varchar(100) NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_dict_data (sort, label, value, dict_type, deleted) VALUES
  (1, '男', '1', 'system_user_sex', 0),
  (2, '女', '2', 'system_user_sex', 0),
  (3, '女-旧', '2', 'system_user_sex', 0);
`
