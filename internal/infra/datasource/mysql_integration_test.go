//go:build integration

package datasource

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestDataSourceConfigOnMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })
	host, err := mysqlC.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := mysqlC.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE infra_data_source_config (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 name varchar(100) NOT NULL,
 url varchar(1024) NOT NULL,
 username varchar(255) NOT NULL,
 password varchar(255) NOT NULL,
 create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
 deleted bit(1) NOT NULL DEFAULT 0
)`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Store: store, Key: testEncryptorPassword, Master: MasterFromDSN(dsn)}
	jdbcURL := "jdbc:mysql://" + host + ":" + port.Port() + "/ruoyi-vue-pro"
	id, err := svc.Create(ctx, SaveInput{Name: "从库", URL: jdbcURL, Username: "root", Password: "123456"})
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	var secret string
	if err := db.QueryRow(`SELECT password FROM infra_data_source_config WHERE id=?`, id).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptBase64(testEncryptorPassword, secret)
	if err != nil || plain != "123456" || secret == "123456" {
		t.Fatalf("密码应以密文保存：%s %v", secret, err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != 0 || list[1].Username != "root" {
		t.Fatalf("%+v %v", list, err)
	}
	if err := svc.DeleteList(ctx, []int64{id, 99}); err != nil {
		t.Fatal(err)
	}
	if item, err := svc.Get(ctx, id); err != nil || item != nil {
		t.Fatalf("删除后应为空：%+v %v", item, err)
	}
}
