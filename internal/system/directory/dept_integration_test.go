//go:build integration

package directory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestDeptGetAndDeleteMySQL 用真实事务验证部门详情、租户边界和整批删除。
func TestDeptGetAndDeleteMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("dept_contract"),
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
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, deptContractSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Depts: store}

	dept, err := svc.GetDept(ctx, 1, 2)
	if err != nil || dept == nil || dept.ID != 2 || dept.Name != "研发" || dept.ParentID != 1 ||
		dept.LeaderUserID == nil || *dept.LeaderUserID != 17 || dept.CreateTime <= 0 {
		t.Fatalf("详情未完整映射：dept=%+v err=%v", dept, err)
	}
	for _, id := range []int64{4, 5, 777} {
		dept, err := svc.GetDept(ctx, 1, id)
		if err != nil || dept != nil {
			t.Fatalf("跨租户、软删或缺失详情泄漏：id=%d dept=%+v err=%v", id, dept, err)
		}
	}
	for _, id := range []int64{4, 5, 777} {
		if got := rpcErrorCode(svc.DeleteDept(ctx, 1, id)); got != codeDeptNotFound {
			t.Fatalf("单删缺失/跨租户应报部门不存在：id=%d code=%d", id, got)
		}
	}
	if got := rpcErrorCode(svc.DeleteDept(ctx, 1, 1)); got != codeDeptHasChild {
		t.Fatalf("有子部门的单删未阻断：code=%d", got)
	}
	assertDeptDeleted(t, db, 1, 0)
	// 其他租户的子部门不能影响本租户，反过来本租户也不能删除其他租户数据。
	if err := svc.DeleteDeptList(ctx, 1, []int64{4}); err != nil {
		t.Fatalf("批删其他租户 ID 应像 Java 一样忽略：%v", err)
	}
	assertDeptDeleted(t, db, 4, 0)
	if got := rpcErrorCode(svc.DeleteDept(ctx, 2, 4)); got != codeDeptHasChild {
		t.Fatalf("其他租户自己的子部门检查缺失：code=%d", got)
	}

	// Java 批删即使传入不存在的 ID，也会检查它是否仍有直接子部门。
	if got := rpcErrorCode(svc.DeleteDeptList(ctx, 1, []int64{2, 6, 99})); got != codeDeptHasChild {
		t.Fatalf("孤儿子部门未阻断整批：code=%d", got)
	}
	assertDeptDeleted(t, db, 2, 0)
	assertDeptDeleted(t, db, 6, 0)
	if err := svc.DeleteDeptList(ctx, 1, []int64{2, 6, 777, 2}); err != nil {
		t.Fatalf("无子部门的合法批删失败：%v", err)
	}
	assertDeptDeleted(t, db, 2, 1)
	assertDeptDeleted(t, db, 6, 1)
	if err := svc.DeleteDept(ctx, 1, 1); err != nil {
		t.Fatalf("已软删子部门不应阻断父部门：%v", err)
	}
	assertDeptDeleted(t, db, 1, 1)
	if got := rpcErrorCode(svc.DeleteDept(ctx, 1, 1)); got != codeDeptNotFound {
		t.Fatalf("重复单删未报告不存在：code=%d", got)
	}

	// 第二行更新故意失败，验证第一行不会部分提交。
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER reject_dept_nine BEFORE UPDATE ON system_dept FOR EACH ROW
		BEGIN IF NEW.id=9 AND NEW.deleted=b'1' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='blocked'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteDeptList(ctx, 1, []int64{8, 9}); err == nil {
		t.Fatal("更新中途失败后批删不应报告成功")
	}
	assertDeptDeleted(t, db, 8, 0)
	assertDeptDeleted(t, db, 9, 0)
	if err := svc.DeleteDeptList(ctx, 1, nil); err != nil {
		t.Fatalf("空列表应无副作用：%v", err)
	}
}

func assertDeptDeleted(t *testing.T, db *sql.DB, id int64, want int) {
	t.Helper()
	var deleted int
	if err := db.QueryRow(`SELECT deleted+0 FROM system_dept WHERE id=?`, id).Scan(&deleted); err != nil || deleted != want {
		t.Fatalf("部门 %d 的 deleted=%d，预期 %d：%v", id, deleted, want, err)
	}
}

const deptContractSchema = `
CREATE TABLE system_dept (
  id bigint PRIMARY KEY,
  name varchar(30) NOT NULL,
  parent_id bigint NOT NULL DEFAULT 0,
  sort int NOT NULL DEFAULT 0,
  leader_user_id bigint NULL,
  phone varchar(11) NULL,
  email varchar(50) NULL,
  status tinyint NOT NULL,
  create_time datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted bit(1) NOT NULL DEFAULT b'0',
  tenant_id bigint NOT NULL
);
INSERT INTO system_dept (id,name,parent_id,sort,leader_user_id,status,deleted,tenant_id) VALUES
 (1,'总部',0,1,NULL,0,0,1),
 (2,'研发',1,2,17,0,0,1),
 (3,'设计',0,3,NULL,0,0,1),
 (4,'其他租户',0,4,NULL,0,0,2),
 (5,'已删除',0,5,NULL,0,1,1),
 (6,'运维',0,6,NULL,0,0,1),
 (7,'孤儿子部门',99,7,NULL,0,0,1),
 (8,'测试一',0,8,NULL,0,0,1),
 (9,'测试二',0,9,NULL,0,0,1),
 (10,'其他租户子部门',4,10,NULL,0,0,2);
`
