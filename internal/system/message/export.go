package message

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// SmsExportFilter 对齐短信模板分页的筛选，导出不再套用每页条数上限。
type SmsExportFilter struct {
	Type          *int
	Status        *int
	Code          string
	Content       string
	APITemplateID string
	ChannelID     *int64
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

// SmsLogExportFilter 对齐短信日志分页的筛选。
type SmsLogExportFilter struct {
	ChannelID     *int64
	TemplateID    *int64
	Mobile        string
	SendStatus    *int
	SendFrom      *time.Time
	SendTo        *time.Time
	ReceiveStatus *int
	ReceiveFrom   *time.Time
	ReceiveTo     *time.Time
}

// DictText 按字典值取显示名。重复值保留排序靠前的第一条。
func (m *MySQL) DictText(ctx context.Context, dictType string) (map[string]string, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT value, label FROM system_dict_data
		WHERE dict_type=? AND deleted=0 ORDER BY sort, id`, dictType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := map[string]string{}
	for rows.Next() {
		var value, label string
		if err := rows.Scan(&value, &label); err != nil {
			return nil, err
		}
		if _, ok := labels[value]; !ok {
			labels[value] = label
		}
	}
	return labels, rows.Err()
}

func (m *MySQL) SmsExportRows(ctx context.Context, filter SmsExportFilter, emit func(SmsTemplate) error) error {
	where, args := smsExportWhere(filter)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, type, status, code, name, content, IFNULL(remark,''), api_template_id, channel_id, channel_code, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
		FROM system_sms_template `+where+` ORDER BY id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item SmsTemplate
		if err := rows.Scan(&item.ID, &item.Type, &item.Status, &item.Code, &item.Name, &item.Content, &item.Remark, &item.APITemplateID, &item.ChannelID, &item.ChannelCode, &item.CreateTime); err != nil {
			return err
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func smsExportWhere(filter SmsExportFilter) (string, []any) {
	where := `WHERE deleted=0`
	var args []any
	if filter.Type != nil {
		where += ` AND type=?`
		args = append(args, *filter.Type)
	}
	if filter.Status != nil {
		where += ` AND status=?`
		args = append(args, *filter.Status)
	}
	where, args = like(where, args, "code", filter.Code)
	where, args = like(where, args, "content", filter.Content)
	where, args = like(where, args, "api_template_id", filter.APITemplateID)
	if filter.ChannelID != nil {
		where += ` AND channel_id=?`
		args = append(args, *filter.ChannelID)
	}
	where, args = between(where, args, "create_time", filter.CreatedFrom, filter.CreatedTo)
	return where, args
}

// SmsLogRow 是导出需要的短信日志字段。
type SmsLogRow struct {
	ID              int64
	ChannelID       int64
	ChannelCode     string
	TemplateID      int64
	TemplateCode    string
	TemplateType    int
	TemplateContent string
	TemplateParams  string
	APITemplateID   string
	Mobile          string
	UserID          int64
	UserType        int
	SendStatus      int
	SendTime        *int64
	APISendCode     string
	APISendMsg      string
	APIRequestID    string
	APISerialNo     string
	ReceiveStatus   int
	ReceiveTime     *int64
	APIReceiveCode  string
	APIReceiveMsg   string
	CreateTime      int64
}

func (m *MySQL) SmsLogExportRows(ctx context.Context, filter SmsLogExportFilter, emit func(SmsLogRow) error) error {
	where, args := smsLogExportWhere(filter)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, channel_id, channel_code, template_id, template_code, template_type,
		IFNULL(template_content,''), IFNULL(template_params,''), IFNULL(api_template_id,''), mobile, user_id, user_type,
		send_status, UNIX_TIMESTAMP(send_time)*1000, IFNULL(api_send_code,''), IFNULL(api_send_msg,''), IFNULL(api_request_id,''), IFNULL(api_serial_no,''),
		receive_status, UNIX_TIMESTAMP(receive_time)*1000, IFNULL(api_receive_code,''), IFNULL(api_receive_msg,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000
		FROM system_sms_log `+where+` ORDER BY id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item SmsLogRow
		var sent, received sql.NullInt64
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.ChannelCode, &item.TemplateID, &item.TemplateCode, &item.TemplateType,
			&item.TemplateContent, &item.TemplateParams, &item.APITemplateID, &item.Mobile, &item.UserID, &item.UserType,
			&item.SendStatus, &sent, &item.APISendCode, &item.APISendMsg, &item.APIRequestID, &item.APISerialNo,
			&item.ReceiveStatus, &received, &item.APIReceiveCode, &item.APIReceiveMsg, &item.CreateTime); err != nil {
			return err
		}
		if sent.Valid && sent.Int64 > 0 {
			item.SendTime = &sent.Int64
		}
		if received.Valid && received.Int64 > 0 {
			item.ReceiveTime = &received.Int64
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func smsLogExportWhere(filter SmsLogExportFilter) (string, []any) {
	where := `WHERE deleted=0`
	var args []any
	if filter.ChannelID != nil {
		where += ` AND channel_id=?`
		args = append(args, *filter.ChannelID)
	}
	if filter.TemplateID != nil {
		where += ` AND template_id=?`
		args = append(args, *filter.TemplateID)
	}
	where, args = like(where, args, "mobile", filter.Mobile)
	if filter.SendStatus != nil {
		where += ` AND send_status=?`
		args = append(args, *filter.SendStatus)
	}
	where, args = between(where, args, "send_time", filter.SendFrom, filter.SendTo)
	if filter.ReceiveStatus != nil {
		where += ` AND receive_status=?`
		args = append(args, *filter.ReceiveStatus)
	}
	where, args = between(where, args, "receive_time", filter.ReceiveFrom, filter.ReceiveTo)
	return where, args
}

func like(where string, args []any, column, value string) (string, []any) {
	if value == "" {
		return where, args
	}
	return where + ` AND ` + column + ` LIKE ?`, append(args, "%"+value+"%")
}

func between(where string, args []any, column string, from, to *time.Time) (string, []any) {
	if from != nil {
		where += ` AND ` + column + ` >= ?`
		args = append(args, *from)
	}
	if to != nil {
		where += ` AND ` + column + ` <= ?`
		args = append(args, *to)
	}
	return where, args
}

func labelOf(labels map[string]string, value string) string {
	if text, ok := labels[value]; ok {
		return text
	}
	return value
}

func labelInt(labels map[string]string, value int) string {
	return labelOf(labels, strconv.Itoa(value))
}

func excelMillis(millis *int64) string {
	if millis == nil || *millis <= 0 {
		return ""
	}
	return time.UnixMilli(*millis).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")
}

func excelMillisValue(millis int64) string {
	return excelMillis(&millis)
}
