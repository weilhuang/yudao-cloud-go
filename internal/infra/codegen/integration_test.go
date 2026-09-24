//go:build integration

package codegen

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestCodegenImportSyncAndPreview(t *testing.T) {
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
	if _, err := db.Exec(codegenSchema); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db, Author: func(context.Context, int64) (string, error) { return "芋道", nil }}
	if _, err := svc.CreateList(ctx, 1, 0, []string{"demo_blank"}); err == nil || err.(*Error).Msg != "数据库的表注释未填写" {
		t.Fatal(err)
	}
	ids, err := svc.CreateList(ctx, 1, 0, []string{"demo_student"})
	if err != nil || len(ids) != 1 {
		t.Fatal(err, ids)
	}
	if _, err := svc.CreateList(ctx, 1, 0, []string{"demo_student"}); err == nil || err.(*Error).Msg != "表定义已经存在" {
		t.Fatal(err)
	}
	tables, err := svc.DatabaseTables(ctx, 0, "demo_", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if table.Name == "demo_student" || table.Name == "qrtz_job" {
			t.Fatalf("不应再列出已导入或被排除的表: %+v", tables)
		}
	}
	page, err := svc.Page(ctx, PageQuery{PageNo: 1, PageSize: 10, TableName: "demo_student"})
	if err != nil || page.Total != 1 || page.List[0].ClassName != "Student" || page.List[0].Author != "芋道" {
		t.Fatalf("%+v %v", page, err)
	}
	_, columns, err := svc.Detail(ctx, ids[0])
	if err != nil || len(columns) < 2 || columns[0].JavaField != "id" || columns[0].OrdinalPosition != 1 {
		t.Fatalf("%+v %v", columns, err)
	}
	if err := svc.Sync(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	files, err := svc.Preview(ctx, ids[0])
	if err != nil || len(files) != 14 || files[0].FilePath != "sql/sql.sql" || !strings.Contains(files[0].Code, "INSERT INTO system_menu") || !containsCode(files, "StudentDO") || !containsCode(files, "StudentPageReqVO") || !containsCode(files, "StudentServiceImpl") || !containsCode(files, "StudentApi") || !containsCode(files, "StudentForm") || !containsCode(files, "handleExport") {
		t.Fatalf("%+v %v", files, err)
	}
	body, err := svc.Download(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(zr.File) != 14 {
		t.Fatal(err)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	text, _ := io.ReadAll(rc)
	if !strings.Contains(string(text), "INSERT INTO system_menu") {
		t.Fatalf("%s", text)
	}
	if err := svc.Delete(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Preview(ctx, ids[0]); err == nil || err.(*Error).Msg != "表定义不存在" {
		t.Fatal(err)
	}
}

const codegenSchema = `
CREATE TABLE infra_codegen_table (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  data_source_config_id bigint NOT NULL,
  scene tinyint NOT NULL DEFAULT 1,
  table_name varchar(200) NOT NULL DEFAULT '',
  table_comment varchar(500) NOT NULL DEFAULT '',
  remark varchar(500) NULL,
  module_name varchar(30) NOT NULL,
  business_name varchar(30) NOT NULL,
  class_name varchar(100) NOT NULL DEFAULT '',
  class_comment varchar(50) NOT NULL,
  author varchar(50) NOT NULL,
  template_type tinyint NOT NULL DEFAULT 1,
  front_type tinyint NOT NULL,
  parent_menu_id bigint NULL,
  master_table_id bigint NULL,
  sub_join_column_id bigint NULL,
  sub_join_many bit(1) NULL,
  tree_parent_column_id bigint NULL,
  tree_name_column_id bigint NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE infra_codegen_column (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  table_id bigint NOT NULL,
  column_name varchar(200) NOT NULL,
  data_type varchar(100) NOT NULL,
  column_comment varchar(500) NOT NULL,
  nullable bit(1) NOT NULL,
  primary_key bit(1) NOT NULL,
  ordinal_position int NOT NULL,
  java_type varchar(32) NOT NULL,
  java_field varchar(64) NOT NULL,
  dict_type varchar(200) NULL DEFAULT '',
  example varchar(64) NULL,
  create_operation bit(1) NOT NULL,
  update_operation bit(1) NOT NULL,
  list_operation bit(1) NOT NULL,
  list_operation_condition varchar(32) NOT NULL DEFAULT '=',
  list_operation_result bit(1) NOT NULL,
  html_type varchar(32) NOT NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE demo_student (
  id bigint PRIMARY KEY COMMENT '编号',
  name varchar(30) NOT NULL COMMENT '名字',
  status tinyint NOT NULL COMMENT '状态'
) COMMENT '学生表';
CREATE TABLE demo_blank (
  id bigint PRIMARY KEY
);
CREATE TABLE qrtz_job (
  id bigint PRIMARY KEY COMMENT '编号'
) COMMENT '定时任务';
`

func containsCode(files []PreviewFile, text string) bool {
	for _, file := range files {
		if strings.Contains(file.Code, text) {
			return true
		}
	}
	return false
}
