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

// TestSaveDeptParentMySQL 验证父链校验在真实租户、软删除数据和并发改父下仍成立。
func TestSaveDeptParentMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("dept_parent"),
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
	if _, err := db.ExecContext(ctx, `INSERT INTO system_dept
		(id,name,parent_id,sort,status,deleted,tenant_id) VALUES (11,'研发二组',2,11,0,0,1)`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Depts: store}
	for _, tt := range []struct {
		name     string
		tenantID int64
		dept     Dept
		wantCode int
	}{
		{"自己作为父部门", 1, Dept{ID: 1, Name: "总部", ParentID: 1}, codeDeptParentSelf},
		{"直接子部门作为父部门", 1, Dept{ID: 1, Name: "总部", ParentID: 2}, codeDeptParentIsChild},
		{"孙部门作为父部门", 1, Dept{ID: 1, Name: "总部", ParentID: 11}, codeDeptParentIsChild},
		{"跨租户父部门", 1, Dept{ID: 1, Name: "总部", ParentID: 4}, codeDeptParentNotFound},
		{"已删除父部门", 1, Dept{ID: 1, Name: "总部", ParentID: 5}, codeDeptParentNotFound},
		{"不存在的更新目标", 1, Dept{ID: 999, Name: "缺失"}, codeDeptNotFound},
		{"另一租户挂到本租户", 2, Dept{ID: 4, Name: "其他租户", ParentID: 1}, codeDeptParentNotFound},
		{"新建时跨租户父部门", 1, Dept{Name: "新建", ParentID: 4}, codeDeptParentNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.SaveDept(ctx, tt.tenantID, tt.dept); rpcErrorCode(err) != tt.wantCode {
				t.Fatalf("错误码不符：got=%d want=%d err=%v", rpcErrorCode(err), tt.wantCode, err)
			}
			assertDeptParent(t, db, 1, 0)
			assertDeptParent(t, db, 4, 0)
		})
	}
	if _, err := svc.SaveDept(ctx, 1, Dept{ID: 3, Name: "设计", ParentID: 2}); err != nil {
		t.Fatalf("合法改父失败：%v", err)
	}
	assertDeptParent(t, db, 3, 2)

	// 直接调用存储层，模拟两个请求都已通过服务层的旧快照校验。
	// 事务内重新读取并锁定层级后，只能有一个方向提交。
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, dept := range []Dept{
		{ID: 8, Name: "测试一", ParentID: 9},
		{ID: 9, Name: "测试二", ParentID: 8},
	} {
		go func(dept Dept) {
			<-start
			results <- store.UpdateDept(ctx, 1, dept)
		}(dept)
	}
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("并发互设父部门必须恰好成功一个：first=%v second=%v", first, second)
	}
	failed := first
	if failed == nil {
		failed = second
	}
	if rpcErrorCode(failed) != codeDeptParentIsChild {
		t.Fatalf("失败的并发改父应返回子部门环路错误：%v", failed)
	}
	var parent8, parent9 int64
	if err := db.QueryRowContext(ctx, `SELECT parent_id FROM system_dept WHERE id=8`).Scan(&parent8); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT parent_id FROM system_dept WHERE id=9`).Scan(&parent9); err != nil {
		t.Fatal(err)
	}
	if !((parent8 == 9 && parent9 == 0) || (parent8 == 0 && parent9 == 8)) {
		t.Fatalf("并发写入形成了环路或丢失更新：parent8=%d parent9=%d", parent8, parent9)
	}
}

func assertDeptParent(t *testing.T, db *sql.DB, id, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRow(`SELECT parent_id FROM system_dept WHERE id=?`, id).Scan(&got); err != nil || got != want {
		t.Fatalf("部门 %d 的 parent_id=%d，预期 %d：%v", id, got, want, err)
	}
}
