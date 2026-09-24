package message

import "testing"
import "time"

func TestSmsExportWhereKeepsOptionalFilters(t *testing.T) {
	status := 0
	where, args := smsExportWhere(SmsExportFilter{Status: &status, Code: "login"})
	if where != "WHERE deleted=0 AND status=? AND code LIKE ?" || len(args) != 2 || args[1] != "%login%" {
		t.Fatalf("%s %v", where, args)
	}
	if text := labelInt(map[string]string{"1": "验证码"}, 1); text != "验证码" {
		t.Fatal(text)
	}
	if text := labelOf(map[string]string{}, "ALIYUN"); text != "ALIYUN" {
		t.Fatal(text)
	}
	at := time.Date(2026, 9, 23, 10, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	if excelMillisValue(at.UnixMilli()) != "2026-09-23 10:00:00" {
		t.Fatal(excelMillisValue(at.UnixMilli()))
	}
}
