package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// smsCallback 免登录、不校验租户。渠道未配置时不改日志。
func (h *handler) smsCallback(channel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeBiz(c, err)
			return
		}
		if err := h.db.ReceiveSms(c.Request.Context(), channel, string(body)); err != nil {
			writeBiz(c, err)
			return
		}
		httpx.OK(c, true)
	}
}

type smsReceipt struct {
	LogID    int64
	Serial   string
	Success  bool
	Code     string
	Msg      string
	Received string
}

func (m *MySQL) ReceiveSms(ctx context.Context, channel, body string) error {
	var id int64
	err := m.DB.QueryRowContext(ctx, `SELECT id FROM system_sms_channel WHERE code=? AND status=0 AND deleted=0 LIMIT 1`, channel).Scan(&id)
	if err == sql.ErrNoRows {
		return &Error{Code: 500, Msg: "短信客户端(" + channel + ") 不存在"}
	}
	if err != nil {
		return err
	}
	receipts, err := parseSmsReceipts(channel, body)
	if err != nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	for _, item := range receipts {
		if err := m.applyReceipt(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQL) applyReceipt(ctx context.Context, item smsReceipt) error {
	id := item.LogID
	if id == 0 {
		if item.Serial == "" {
			return nil
		}
		err := m.DB.QueryRowContext(ctx, `SELECT id FROM system_sms_log WHERE api_serial_no=? AND deleted=0 LIMIT 1`, item.Serial).Scan(&id)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
	}
	status := 20
	if item.Success {
		status = 10
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE system_sms_log SET receive_status=?, receive_time=?, api_receive_code=?, api_receive_msg=? WHERE id=? AND deleted=0`,
		status, nullTime(item.Received), cut(item.Code, 63), cut(item.Msg, 255), id)
	return err
}

func nullTime(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func parseSmsReceipts(channel, body string) ([]smsReceipt, error) {
	switch channel {
	case "ALIYUN":
		return parseAliyun(body)
	case "TENCENT":
		return parseTencent(body)
	case "HUAWEI":
		return parseHuawei(body)
	case "QINIU":
		return parseQiniu(body)
	default:
		return nil, errUnknownChannel
	}
}

var errUnknownChannel = &Error{Code: 500, Msg: "系统异常"}

func parseAliyun(body string) ([]smsReceipt, error) {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, err
	}
	out := make([]smsReceipt, 0, len(rows))
	for _, row := range rows {
		out = append(out, smsReceipt{
			LogID: jsonInt(row["out_id"]), Serial: jsonString(row["biz_id"]), Success: jsonBool(row["success"]),
			Code: jsonString(row["err_code"]), Msg: jsonString(row["err_msg"]), Received: wallTime(jsonString(row["report_time"])),
		})
	}
	return out, nil
}

func parseTencent(body string) ([]smsReceipt, error) {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, err
	}
	out := make([]smsReceipt, 0, len(rows))
	for _, row := range rows {
		out = append(out, smsReceipt{
			Serial: jsonString(row["sid"]), Success: jsonString(row["report_status"]) == "SUCCESS",
			Code: jsonString(row["errmsg"]), Msg: jsonString(row["description"]), Received: wallTime(jsonString(row["user_receive_time"])),
		})
	}
	return out, nil
}

func parseHuawei(body string) ([]smsReceipt, error) {
	values, err := url.ParseQuery(body)
	if err != nil {
		return nil, err
	}
	received := ""
	if text := values.Get("updateTime"); text != "" {
		t, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, err
		}
		received = t.UTC().Format("2006-01-02 15:04:05")
	}
	logID, _ := strconv.ParseInt(values.Get("extend"), 10, 64)
	return []smsReceipt{{
		LogID: logID, Serial: values.Get("smsMsgId"), Success: values.Get("status") == "DELIVRD",
		Code: values.Get("status"), Msg: values.Get("statusDesc"), Received: received,
	}}, nil
}

func parseQiniu(body string) ([]smsReceipt, error) {
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	out := make([]smsReceipt, 0, len(payload.Items))
	for _, row := range payload.Items {
		received := ""
		if sec := jsonInt(row["delivrd_at"]); sec > 0 {
			received = time.Unix(sec, 0).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
		}
		out = append(out, smsReceipt{
			LogID: jsonInt(row["seq"]), Serial: jsonString(row["message_id"]), Success: jsonString(row["status"]) == "DELIVRD",
			Msg: jsonString(row["status"]), Received: received,
		})
	}
	return out, nil
}

func wallTime(value string) string {
	if value == "" {
		return ""
	}
	if _, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local); err != nil {
		return ""
	}
	return value
}

func jsonString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case float64:
		return strconv.FormatInt(int64(item), 10)
	default:
		return ""
	}
}

func jsonInt(value any) int64 {
	switch item := value.(type) {
	case float64:
		return int64(item)
	case string:
		n, _ := strconv.ParseInt(item, 10, 64)
		return n
	default:
		return 0
	}
}

func jsonBool(value any) bool {
	item, _ := value.(bool)
	return item
}
