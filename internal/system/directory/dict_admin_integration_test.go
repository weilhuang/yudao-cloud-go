//go:build integration

package directory

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// TestDictAdminMySQL 用一次隔离 MySQL 检查字典页详情、筛选、批删原子性和真实 XLSX。
func TestDictAdminMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("dict_admin"),
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
	if _, err := db.ExecContext(ctx, dictAdminSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	svc := &Service{Reader: store, Dicts: store}
	h := &handler{svc: svc}
	r := gin.New()
	r.GET("/type/page", func(c *gin.Context) { h.dictTypePage(c, caller{}) })
	r.GET("/type/get", func(c *gin.Context) { h.dictTypeGet(c, caller{}) })
	r.DELETE("/type/delete-list", func(c *gin.Context) { h.dictTypeDeleteList(c, caller{}) })
	r.GET("/type/export", func(c *gin.Context) { h.dictTypeExport(c, caller{}) })
	r.GET("/data/page", func(c *gin.Context) { h.dictPage(c, caller{}) })
	r.GET("/data/get", func(c *gin.Context) { h.dictDataGet(c, caller{}) })
	r.DELETE("/data/delete-list", func(c *gin.Context) { h.dictDataDeleteList(c, caller{}) })
	r.GET("/data/export", func(c *gin.Context) { h.dictDataExport(c, caller{}) })

	// Java 的类型下拉包含已停用类型；缺失与软删记录的详情返回 null。
	simple, err := store.DictTypeSimple(ctx)
	if err != nil || len(simple) != 6 || simple[1].Status != 1 {
		t.Fatalf("类型下拉遗漏停用项：%+v %v", simple, err)
	}
	for _, path := range []string{"/type/get?id=999", "/type/get?id=14", "/data/get?id=999", "/data/get?id=6"} {
		if body := postCall(r, http.MethodGet, path).Body.String(); !strings.Contains(body, `"data":null`) {
			t.Fatalf("%s 应返回 null：%s", path, body)
		}
	}
	if body := postCall(r, http.MethodGet, "/type/get?id=11").Body.String(); !strings.Contains(body, `"type":"system_user_sex"`) || !strings.Contains(body, `"createTime":`) {
		t.Fatalf("类型详情字段不完整：%s", body)
	}
	if body := postCall(r, http.MethodGet, "/data/get?id=1").Body.String(); !strings.Contains(body, `"label":"男"`) || !strings.Contains(body, `"colorType":"primary"`) {
		t.Fatalf("数据详情字段不完整：%s", body)
	}

	// 类型分页按名称模糊、状态与创建时间双端点过滤；数据分页按类型精确匹配且 sort 倒序。
	typePath := "/type/page?name=" + url.QueryEscape("用户") + "&status=0&createTime[0]=" + url.QueryEscape("2026-09-23 09:00:00") + "&createTime[1]=" + url.QueryEscape("2026-09-23 12:00:00")
	typePage := postCall(r, http.MethodGet, typePath).Body.String()
	if !strings.Contains(typePage, `"total":2`) || !strings.Contains(typePage, `"id":15`) || !strings.Contains(typePage, `"id":11`) || strings.Contains(typePage, `"id":12`) {
		t.Fatalf("类型分页过滤失败：%s", typePage)
	}
	dataPage := postCall(r, http.MethodGet, "/data/page?dictType=system_user_sex&pageSize=1").Body.String()
	if !strings.Contains(dataPage, `"total":2`) || !strings.Contains(dataPage, `"id":2`) || strings.Contains(dataPage, `"id":1`) {
		t.Fatalf("数据分页排序或类型精确过滤失败：%s", dataPage)
	}
	for _, path := range []string{"/type/page?pageSize=201", "/type/page?createTime[0]=bad", "/data/page?status=2", "/data/page?pageNo=0"} {
		if body := postCall(r, http.MethodGet, path).Body.String(); !strings.Contains(body, `"code":400`) {
			t.Fatalf("非法参数未拒绝：%s => %s", path, body)
		}
	}

	// 导出忽略请求 pageSize，但保留名称、类型、状态筛选；Excel 列严格按 Java @ExcelProperty。
	typeXLSX := postCall(r, http.MethodGet, "/type/export?name="+url.QueryEscape("用户")+"&pageSize=1")
	if typeXLSX.Header().Get("Content-Type") != "application/vnd.ms-excel;charset=UTF-8" ||
		typeXLSX.Header().Get("Content-Disposition") != "attachment;filename="+url.QueryEscape("字典类型.xls") {
		t.Fatalf("类型导出下载头不符：%v", typeXLSX.Header())
	}
	typeXML := dictSheetXML(t, typeXLSX.Body.Bytes())
	if strings.Count(typeXML, "<row ") != 4 || !strings.Contains(typeXML, "字典主键") || !strings.Contains(typeXML, "关闭") || strings.Contains(typeXML, "备注") {
		t.Fatalf("类型导出列/过滤/状态不符：%s", typeXML)
	}
	dataXLSX := postCall(r, http.MethodGet, "/data/export?dictType=system_user_sex&pageSize=1")
	if dataXLSX.Header().Get("Content-Disposition") != "attachment;filename="+url.QueryEscape("字典数据.xls") {
		t.Fatalf("数据导出文件名不符：%v", dataXLSX.Header())
	}
	dataXML := dictSheetXML(t, dataXLSX.Body.Bytes())
	if strings.Count(dataXML, "<row ") != 3 || !strings.Contains(dataXML, "字典编码") || !strings.Contains(dataXML, "女") || strings.Contains(dataXML, "金") {
		t.Fatalf("数据导出列/过滤不符：%s", dataXML)
	}
	formulaXML := dictSheetXML(t, postCall(r, http.MethodGet, "/data/export?dictType=system_user_level").Body.Bytes())
	if !strings.Contains(formulaXML, "=1+1") || strings.Contains(formulaXML, "<f>") {
		t.Fatalf("用户输入不应成为 Excel 公式：%s", formulaXML)
	}

	// 有子数据时，整批类型均不得删除；随后成功批删需记录 deleted_time。
	if body := postCall(r, http.MethodDelete, "/type/delete-list?ids=11,15").Body.String(); !strings.Contains(body, `"code":1002006005`) {
		t.Fatalf("带子项的类型批删未拒绝：%s", body)
	}
	assertDictTypeDeleted(t, db, 15, 0, false)
	if body := postCall(r, http.MethodDelete, "/type/delete-list?ids=15,999,15").Body.String(); !strings.Contains(body, `"data":true`) {
		t.Fatalf("无子项类型批删失败：%s", body)
	}
	assertDictTypeDeleted(t, db, 15, 1, true)
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER block_dict_type BEFORE UPDATE ON system_dict_type FOR EACH ROW
		BEGIN IF NEW.id=17 AND NEW.deleted=1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='blocked'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	if body := postCall(r, http.MethodDelete, "/type/delete-list?ids=13,17").Body.String(); !strings.Contains(body, `"code":500`) {
		t.Fatalf("类型批删更新失败未返回错误：%s", body)
	}
	assertDictTypeDeleted(t, db, 13, 0, false)
	assertDictTypeDeleted(t, db, 17, 0, false)

	// 单条 UPDATE 遇触发器报错时全部回滚；不存在 id 按 Java 行为跳过。
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER block_dict_data BEFORE UPDATE ON system_dict_data FOR EACH ROW
		BEGIN IF NEW.id=3 AND NEW.deleted=1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='blocked'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	if body := postCall(r, http.MethodDelete, "/data/delete-list?ids=1,3").Body.String(); !strings.Contains(body, `"code":500`) {
		t.Fatalf("触发器失败未回滚：%s", body)
	}
	assertDictDataDeleted(t, db, 1, 0)
	assertDictDataDeleted(t, db, 3, 0)
	if body := postCall(r, http.MethodDelete, "/data/delete-list?ids=1,2,999,1").Body.String(); !strings.Contains(body, `"data":true`) {
		t.Fatalf("字典数据批删失败：%s", body)
	}
	assertDictDataDeleted(t, db, 1, 1)
	assertDictDataDeleted(t, db, 2, 1)
}

func dictSheetXML(t *testing.T, payload []byte) string {
	t.Helper()
	if !bytes.HasPrefix(payload, []byte("PK")) {
		t.Fatalf("导出不是 XLSX：%q", payload)
	}
	book, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range book.File {
		if file.Name == "xl/worksheets/sheet1.xml" {
			reader, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			return string(body)
		}
	}
	t.Fatal("XLSX 缺少工作表")
	return ""
}

func assertDictTypeDeleted(t *testing.T, db *sql.DB, id int64, want int, wantTime bool) {
	t.Helper()
	var deleted int
	var deletedAt sql.NullTime
	if err := db.QueryRow(`SELECT deleted+0, deleted_time FROM system_dict_type WHERE id=?`, id).Scan(&deleted, &deletedAt); err != nil || deleted != want || deletedAt.Valid != wantTime {
		t.Fatalf("类型 %d deleted=%d time=%v want=%d/%v err=%v", id, deleted, deletedAt, want, wantTime, err)
	}
}

func assertDictDataDeleted(t *testing.T, db *sql.DB, id int64, want int) {
	t.Helper()
	var deleted int
	if err := db.QueryRow(`SELECT deleted+0 FROM system_dict_data WHERE id=?`, id).Scan(&deleted); err != nil || deleted != want {
		t.Fatalf("数据 %d deleted=%d want=%d err=%v", id, deleted, want, err)
	}
}

const dictAdminSchema = `
CREATE TABLE system_dict_type (
 id BIGINT PRIMARY KEY, name VARCHAR(100) NOT NULL, type VARCHAR(100) NOT NULL,
 status TINYINT NOT NULL, remark VARCHAR(500), create_time DATETIME NOT NULL,
 deleted BIT(1) NOT NULL DEFAULT 0, deleted_time DATETIME NULL
);
CREATE TABLE system_dict_data (
 id BIGINT PRIMARY KEY, sort INT NOT NULL, label VARCHAR(100) NOT NULL, value VARCHAR(100) NOT NULL,
 dict_type VARCHAR(100) NOT NULL, status TINYINT NOT NULL, color_type VARCHAR(100), css_class VARCHAR(100),
 remark VARCHAR(500), create_time DATETIME NOT NULL, deleted BIT(1) NOT NULL DEFAULT 0
);
INSERT INTO system_dict_type VALUES
 (11,'用户性别','system_user_sex',0,'可编辑','2026-09-23 10:00:00',0,NULL),
 (12,'用户等级','system_user_level',1,'已停用','2026-09-23 11:00:00',0,NULL),
 (13,'通知类别','system_notify',0,'旧数据','2026-09-22 10:00:00',0,NULL),
 (14,'用户旧数据','old',0,'软删除','2026-09-23 10:00:00',1,NULL),
 (15,'用户缓存','system_cache',0,'无子项','2026-09-23 10:00:00',0,NULL),
 (16,'系统状态','common_status',0,'字典转换','2026-09-23 10:00:00',0,NULL),
 (17,'系统空白','system_empty',0,'回滚检查','2026-09-23 10:00:00',0,NULL);
INSERT INTO system_dict_data VALUES
 (1,1,'男','1','system_user_sex',0,'primary','',NULL,'2026-09-23 10:00:00',0),
 (2,3,'女','2','system_user_sex',1,'danger','',NULL,'2026-09-23 10:00:00',0),
 (3,2,'金','gold','system_user_level',0,'warning','',NULL,'2026-09-23 10:00:00',0),
 (4,1,'开启','0','common_status',0,'','',NULL,'2026-09-23 10:00:00',0),
 (5,2,'关闭','1','common_status',1,'','',NULL,'2026-09-23 10:00:00',0),
 (6,4,'旧数据','x','system_user_sex',0,'','',NULL,'2026-09-23 10:00:00',1),
 (7,4,'=1+1','formula','system_user_level',0,'','',NULL,'2026-09-23 10:00:00',0);
`
