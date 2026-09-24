//go:build integration

package demo03

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestDemo03StudentMySQL(t *testing.T) {
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
	if _, err := db.Exec(demo03Schema); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: db}
	id, err := svc.Create(ctx, 1, Save{
		Name: "小白", Sex: 1, Birthday: 1700000000000, Description: "简介", WithChild: true,
		Courses: []Course{{Name: "语文", Score: 66}, {Name: "数学", Score: 22}},
		Grade:   &Grade{Name: "三年级", Teacher: "周杰伦"},
	})
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	courses, err := svc.Courses(ctx, 1, id)
	if err != nil || len(courses) != 2 || courses[0].Name != "语文" {
		t.Fatalf("%+v %v", courses, err)
	}
	grade, err := svc.GradeByStudent(ctx, 1, id)
	if err != nil || grade == nil || grade.Teacher != "周杰伦" {
		t.Fatalf("%+v %v", grade, err)
	}
	if err := svc.Update(ctx, 1, Save{
		ID: id, Name: "小白", Sex: 1, Birthday: 1700000000000, Description: "改过", WithChild: true,
		Courses: []Course{{ID: courses[0].ID, Name: "语文", Score: 90}},
		Grade:   &Grade{ID: grade.ID, Name: "三年级", Teacher: "周杰伦"},
	}); err != nil {
		t.Fatal(err)
	}
	courses, err = svc.Courses(ctx, 1, id)
	if err != nil || len(courses) != 1 || courses[0].Score != 90 {
		t.Fatalf("课程差集不对: %+v %v", courses, err)
	}
	if _, err := svc.CreateGrade(ctx, 1, Grade{StudentID: id, Name: "另一班", Teacher: "老师"}); err == nil || err.(*Error).Msg != "学生班级已存在" {
		t.Fatal(err)
	}
	erp, err := svc.Create(ctx, 1, Save{Name: "大黑", Sex: 2, Birthday: 1700000000000, Description: "erp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCourse(ctx, 1, Course{StudentID: erp, Name: "体育", Score: 80}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.Page(ctx, 1, Query{PageNo: 1, PageSize: 10, Description: "erp"})
	if err != nil || page.Total != 1 || page.List[0].ID != erp {
		t.Fatalf("%+v %v", page, err)
	}
	if err := svc.Delete(ctx, 1, id); err != nil {
		t.Fatal(err)
	}
	left, err := svc.Courses(ctx, 1, id)
	if err != nil || len(left) != 0 {
		t.Fatalf("删除学生后课程还在: %+v %v", left, err)
	}
	if got, err := svc.Get(ctx, 2, erp); err != nil || got != nil {
		t.Fatalf("其他租户不应读到: %+v %v", got, err)
	}
}

const demo03Schema = `
CREATE TABLE yudao_demo03_student (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  name varchar(100) NOT NULL DEFAULT '',
  sex tinyint NOT NULL,
  birthday datetime NOT NULL,
  description varchar(255) NOT NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL DEFAULT 0
);
CREATE TABLE yudao_demo03_course (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  student_id bigint NOT NULL,
  name varchar(100) NOT NULL DEFAULT '',
  score tinyint NOT NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL DEFAULT 0
);
CREATE TABLE yudao_demo03_grade (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  student_id bigint NOT NULL,
  name varchar(100) NOT NULL DEFAULT '',
  teacher varchar(255) NOT NULL,
  creator varchar(64) NULL DEFAULT '',
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updater varchar(64) NULL DEFAULT '',
  update_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT 0,
  tenant_id bigint NOT NULL DEFAULT 0
);
`
