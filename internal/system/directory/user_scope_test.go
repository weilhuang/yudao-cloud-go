package directory

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAccessSQLMatchesJavaExpression(t *testing.T) {
	sql, args := accessSQL(nil)
	if sql != "" || args != nil {
		t.Fatalf("未登录上下文不加条件：%q %v", sql, args)
	}
	sql, args = accessSQL(&UserAccess{All: true, DeptIDs: []int64{1}})
	if sql != "" || len(args) != 0 {
		t.Fatalf("全部数据权限不加条件：%q %v", sql, args)
	}
	sql, args = accessSQL(&UserAccess{})
	if sql != " AND 1=0" || args != nil {
		t.Fatalf("空范围应查不到数据：%q %v", sql, args)
	}
	sql, args = accessSQL(&UserAccess{Self: true, UserID: 8})
	if sql != " AND u.id=?" || len(args) != 1 || args[0] != int64(8) {
		t.Fatalf("仅本人：%q %v", sql, args)
	}
	sql, args = accessSQL(&UserAccess{DeptIDs: []int64{10, 11}, Self: true, UserID: 8})
	if sql != " AND (u.dept_id IN (?,?) OR u.id=?)" || len(args) != 3 {
		t.Fatalf("部门或本人：%q %v", sql, args)
	}
}

func TestFillUserQueryParsesRoleAndCreateTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, "/admin-api/system/user/page?roleId=9&deptId=10&createTime=2026-01-02%2015:04:05&createTime=2026-01-03%2015:04:05", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	query := UserQuery{}
	if err := fillUserQuery(c, &query); err != nil {
		t.Fatal(err)
	}
	if query.RoleID == nil || *query.RoleID != 9 || query.DeptID == nil || *query.DeptID != 10 {
		t.Fatalf("%+v", query)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	if query.CreatedFrom == nil || !query.CreatedFrom.Equal(time.Date(2026, 1, 2, 15, 4, 5, 0, loc)) {
		t.Fatalf("开始时间 %+v", query.CreatedFrom)
	}
	if query.CreatedTo == nil || !query.CreatedTo.Equal(time.Date(2026, 1, 3, 15, 4, 5, 0, loc)) {
		t.Fatalf("结束时间 %+v", query.CreatedTo)
	}
	badReq := httptest.NewRequest(http.MethodGet, "/admin-api/system/user/page?createTime=yesterday", nil)
	bad, _ := gin.CreateTestContext(httptest.NewRecorder())
	bad.Request = badReq
	if err := fillUserQuery(bad, &UserQuery{}); err == nil {
		t.Fatal("非法创建时间应拒绝")
	}
}
