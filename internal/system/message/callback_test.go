package message

import "testing"

func TestParseSmsReceipts(t *testing.T) {
	aliyun, err := parseSmsReceipts("ALIYUN", `[{"success":true,"err_code":"DELIVERED","err_msg":"ok","biz_id":"biz-1","out_id":"9","report_time":"2024-01-02 03:04:05"}]`)
	if err != nil || len(aliyun) != 1 || !aliyun[0].Success || aliyun[0].LogID != 9 || aliyun[0].Serial != "biz-1" || aliyun[0].Received != "2024-01-02 03:04:05" {
		t.Fatalf("%+v %v", aliyun, err)
	}
	tencent, err := parseSmsReceipts("TENCENT", `[{"report_status":"SUCCESS","errmsg":"OK","description":"送达","sid":"sid-1","user_receive_time":"2024-01-02 03:04:05"}]`)
	if err != nil || tencent[0].LogID != 0 || tencent[0].Serial != "sid-1" || !tencent[0].Success {
		t.Fatalf("%+v %v", tencent, err)
	}
	huawei, err := parseSmsReceipts("HUAWEI", "status=DELIVRD&statusDesc=ok&smsMsgId=hw-1&extend=7&updateTime=2024-01-02T03:04:05Z")
	if err != nil || huawei[0].LogID != 7 || huawei[0].Received != "2024-01-02 03:04:05" || !huawei[0].Success {
		t.Fatalf("%+v %v", huawei, err)
	}
	qiniu, err := parseSmsReceipts("QINIU", `{"items":[{"status":"DELIVRD","mobile":"15600000000","message_id":"qn-1","seq":8,"delivrd_at":1704157445}]}`)
	if err != nil || qiniu[0].LogID != 8 || qiniu[0].Serial != "qn-1" || qiniu[0].Received == "" {
		t.Fatalf("%+v %v", qiniu, err)
	}
	if _, err := parseSmsReceipts("ALIYUN", `{`); err == nil {
		t.Fatal("坏 JSON 应失败")
	}
}
