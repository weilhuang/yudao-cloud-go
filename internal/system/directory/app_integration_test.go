//go:build integration

package directory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestAppDictAndTenantMySQL(t *testing.T) {
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
	if _, err := db.Exec(appSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	list, err := store.DictEnabledByType(ctx, "system_user_sex")
	if err != nil || len(list) != 1 || list[0].Label != "男" || list[0].Value != "1" {
		t.Fatalf("%+v %v", list, err)
	}
	tenant, err := store.TenantByWebsite(ctx, "www.iocoder.cn")
	if err != nil || tenant == nil || tenant.Status != 1 {
		t.Fatalf("%+v %v", tenant, err)
	}
	open, err := store.TenantByWebsite(ctx, "open.example.com")
	if err != nil || open == nil || open.Name != "开启" {
		t.Fatalf("%+v %v", open, err)
	}
}

const appSchema = `
CREATE TABLE system_dict_data (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  sort int NOT NULL DEFAULT 0,
  label varchar(100) NOT NULL,
  value varchar(100) NOT NULL,
  dict_type varchar(100) NOT NULL,
  status tinyint NOT NULL DEFAULT 0,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_dict_data (sort, label, value, dict_type, status, deleted) VALUES
  (1, '男', '1', 'system_user_sex', 0, 0),
  (2, '停用', '2', 'system_user_sex', 1, 0),
  (1, '开', '0', 'common_status', 0, 0);
CREATE TABLE system_tenant (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  name varchar(30) NOT NULL,
  contact_name varchar(30) NULL,
  contact_mobile varchar(500) NULL,
  status tinyint NOT NULL,
  websites varchar(256) NULL,
  package_id bigint NOT NULL,
  expire_time datetime NULL,
  account_count int NOT NULL,
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_tenant (name, status, websites, package_id, account_count, deleted) VALUES
  ('关闭', 1, '["www.iocoder.cn"]', 0, 10, 0),
  ('开启', 0, '["open.example.com"]', 0, 10, 0);
`
