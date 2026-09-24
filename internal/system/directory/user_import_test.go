package directory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImportFieldErrorMatchesAnnotations(t *testing.T) {
	key, msg, ok := importFieldError(ImportUser{Username: "ab", Mobile: "12"}, 1, "admin123")
	if ok || key != "ab" || !strings.Contains(msg, "username: 用户账号由 数字、字母 组成") || !strings.Contains(msg, "mobile: 手机号格式不正确") {
		t.Fatalf("ok=%v key=%s msg=%s", ok, key, msg)
	}
	key, msg, ok = importFieldError(ImportUser{}, 2, "admin123")
	if ok || key != "第 2 行" || msg != "username: 用户账号不能为空" {
		t.Fatalf("空账号：%s %s %v", key, msg, ok)
	}
	_, _, ok = importFieldError(ImportUser{Username: "yunai", Nickname: "芋道", Email: "yunai@example.com", Mobile: "15601691300"}, 1, "admin123")
	if !ok {
		t.Fatal("模板示例行应通过字段校验")
	}
}

func TestParseImportRowsUsesDictLabels(t *testing.T) {
	grid := [][]string{
		importHeaders,
		{"yunai", "芋道", "10", "yunai@example.com", "15601691300", "男", "开启"},
		{"", "空", "", "", "", "未知", ""},
	}
	rows := parseImportRows(grid, map[string]int{"男": 1}, map[string]int{"开启": 0})
	if len(rows) != 2 || rows[0].Username != "yunai" || rows[0].DeptID == nil || *rows[0].DeptID != 10 || rows[0].Sex == nil || *rows[0].Sex != 1 || rows[0].Status == nil || *rows[0].Status != 0 {
		t.Fatalf("第一行：%+v", rows)
	}
	if rows[1].Sex != nil || rows[1].Status != nil || rows[1].DeptID != nil {
		t.Fatalf("未知标签应保持空：%+v", rows[1])
	}
}

func TestImportResultKeepsFailureOrder(t *testing.T) {
	raw, err := json.Marshal(ImportResult{
		CreateUsernames: []string{"yunai"},
		Failures:        []ImportFailure{{Key: "第 2 行", Reason: "username: 用户账号不能为空"}, {Key: "yuanma", Reason: "手机号已经存在"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"createUsernames":["yunai"]`) || strings.Index(text, "第 2 行") > strings.Index(text, "yuanma") {
		t.Fatal(text)
	}
}
