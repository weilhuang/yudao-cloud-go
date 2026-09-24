//go:build integration

package directory

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestPostPageAndExportMySQL 用隔离 MySQL 验证租户、软删除、筛选、排序和真实 XLSX 下载。
func TestPostPageAndExportMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("post_contract"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
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
	if _, err := db.ExecContext(ctx, postContractSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Access: store}

	page, err := store.PostPage(ctx, 1, PostQuery{PageNo: 1, PageSize: 1, Name: "研发"})
	if err != nil || page.Total != 2 || len(page.List) != 1 || page.List[0].ID != 2 {
		t.Fatalf("分页条件或 id 倒序错误：%+v, %v", page, err)
	}
	status := 0
	page, err = store.PostPage(ctx, 1, PostQuery{PageNo: 1, PageSize: 10, Name: "研发", Code: "dev", Status: &status})
	if err != nil || page.Total != 1 || len(page.List) != 1 || page.List[0].ID != 1 {
		t.Fatalf("模糊与精确过滤错误：%+v, %v", page, err)
	}
	if page.List[0].CreateTime <= 0 || page.List[0].Remark != "一号" {
		t.Fatalf("岗位响应字段不完整：%+v", page.List[0])
	}
	for _, id := range []int64{5, 6, 999} {
		post, err := store.PostGet(ctx, 1, id)
		if err != nil || post != nil {
			t.Fatalf("跨租户、软删除或缺失岗位泄漏：id=%d post=%+v err=%v", id, post, err)
		}
	}
	labels, err := store.PostStatusLabels(ctx)
	if err != nil || labels[0] != "开启" || labels[1] != "关闭" || len(labels) != 2 {
		t.Fatalf("导出字典未包含停用项或混入软删除：%v, %v", labels, err)
	}

	// 导出必须忽略分页大小、保持筛选，输出 Java FastExcel 的 XLSX 内容。
	h := &handler{svc: svc}
	r := gin.New()
	r.GET("/real-export", func(c *gin.Context) { h.postExport(c, caller{tenantID: 1}) })
	w := postCall(r, http.MethodGet, "/real-export?name=%E7%A0%94%E5%8F%91&pageNo=1&pageSize=1")
	if w.Code != http.StatusOK || !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
		t.Fatalf("导出不是 XLSX：HTTP %d，%q", w.Code, w.Body.String())
	}
	workbook, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var sheetData string
	for _, file := range workbook.File {
		if file.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		sheetData = string(body)
	}
	if strings.Count(sheetData, "<row ") != 3 || !strings.Contains(sheetData, "关闭") || !strings.Contains(sheetData, "岗位序号") {
		t.Fatalf("导出筛选、状态或表头不正确：%s", sheetData)
	}

	// Java deleteByIds 对不存在编号静默跳过；一条 SQL 保证同批写入原子性。
	if err := svc.DeletePostList(ctx, 1, []int64{1, 5, 999, 1}); err != nil {
		t.Fatal(err)
	}
	assertPostDeleted(t, db, 1, 1)
	assertPostDeleted(t, db, 5, 0)
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER block_post_delete BEFORE UPDATE ON system_post FOR EACH ROW
		BEGIN IF NEW.id=4 AND NEW.deleted=1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='blocked'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeletePostList(ctx, 1, []int64{2, 4}); err == nil {
		t.Fatal("批量更新触发器失败时不应报告成功")
	}
	assertPostDeleted(t, db, 2, 0)
	assertPostDeleted(t, db, 4, 0)
}

func assertPostDeleted(t *testing.T, db *sql.DB, id int64, want int) {
	t.Helper()
	var deleted int
	if err := db.QueryRow(`SELECT deleted+0 FROM system_post WHERE id=?`, id).Scan(&deleted); err != nil || deleted != want {
		t.Fatalf("岗位 %d deleted=%d want=%d err=%v", id, deleted, want, err)
	}
}

const postContractSchema = `
CREATE TABLE system_post (
 id BIGINT PRIMARY KEY, name VARCHAR(50) NOT NULL, code VARCHAR(64) NOT NULL,
 sort INT NOT NULL, status TINYINT NOT NULL, remark VARCHAR(500),
 create_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0, tenant_id BIGINT NOT NULL
);
CREATE TABLE system_dict_data (
 id BIGINT PRIMARY KEY, value VARCHAR(30), label VARCHAR(50), dict_type VARCHAR(100),
 sort INT NOT NULL, status TINYINT NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0
);
INSERT INTO system_post VALUES
 (1,'研发甲','devA',3,0,'一号','2026-09-23 10:20:30',0,1),
 (2,'研发乙','devB',1,1,'二号','2026-09-23 10:20:31',0,1),
 (3,'设计','design',2,0,'三号','2026-09-23 10:20:32',0,1),
 (4,'风控','risk',4,0,'四号','2026-09-23 10:20:33',0,1),
 (5,'其他租户','foreign',5,0,'跨租户','2026-09-23 10:20:34',0,2),
 (6,'已删除','removed',6,0,'软删除','2026-09-23 10:20:35',1,1);
INSERT INTO system_dict_data VALUES
 (1,'0','开启','common_status',1,0,0),
 (2,'1','关闭','common_status',2,1,0),
 (3,'2','不应出现','common_status',3,0,1);
`
