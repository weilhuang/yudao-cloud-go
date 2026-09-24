package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// MySQL 读写站内信、邮件和短信表。
type MySQL struct {
	DB *sql.DB
}

func (m *MySQL) NotifyByID(ctx context.Context, id int64) (*NotifyTemplate, error) {
	return scanNotify(m.DB.QueryRowContext(ctx, notifySelect+` WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) NotifyByCode(ctx context.Context, code string) (*NotifyTemplate, error) {
	return scanNotify(m.DB.QueryRowContext(ctx, notifySelect+` WHERE code=? AND deleted=0`, code))
}

const notifySelect = `SELECT id, name, code, IFNULL(nickname,''), content, type, IFNULL(params,'[]'), status, IFNULL(remark,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_notify_template`

func scanNotify(row *sql.Row) (*NotifyTemplate, error) {
	var item NotifyTemplate
	var params string
	err := row.Scan(&item.ID, &item.Name, &item.Code, &item.Nickname, &item.Content, &item.Type, &params, &item.Status, &item.Remark, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Params = decodeParams(params)
	return &item, nil
}

func (m *MySQL) NotifyCodeTaken(ctx context.Context, code string, exceptID int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_notify_template WHERE deleted=0 AND code=? AND id<>?`, code, exceptID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateNotify(ctx context.Context, item NotifyTemplate) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_notify_template (name, code, nickname, content, type, params, status, remark, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Name, item.Code, item.Nickname, item.Content, item.Type, encodeParams(item.Params), item.Status, item.Remark, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateNotify(ctx context.Context, item NotifyTemplate) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_notify_template SET name=?, code=?, nickname=?, content=?, type=?, params=?, status=?, remark=? WHERE id=? AND deleted=0`,
		item.Name, item.Code, item.Nickname, item.Content, item.Type, encodeParams(item.Params), item.Status, item.Remark, item.ID)
	return err
}

func (m *MySQL) DeleteNotify(ctx context.Context, id int64) error {
	return m.deleteIDs(ctx, "system_notify_template", []int64{id})
}

func (m *MySQL) DeleteNotifyList(ctx context.Context, ids []int64) error {
	return m.deleteIDs(ctx, "system_notify_template", ids)
}

func (m *MySQL) NotifyPage(ctx context.Context, pageNo, pageSize int, name, code string, status *int) (Page[NotifyTemplate], error) {
	where := `WHERE deleted=0`
	args := []any{}
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if code != "" {
		where += ` AND code LIKE ?`
		args = append(args, "%"+code+"%")
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	return queryNotify(ctx, m.DB, where, args, pageNo, pageSize)
}

func queryNotify(ctx context.Context, db *sql.DB, where string, args []any, pageNo, pageSize int) (Page[NotifyTemplate], error) {
	var total int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_notify_template `+where, args...).Scan(&total); err != nil {
		return Page[NotifyTemplate]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := db.QueryContext(ctx, notifySelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[NotifyTemplate]{}, err
	}
	defer rows.Close()
	list := make([]NotifyTemplate, 0)
	for rows.Next() {
		var item NotifyTemplate
		var params string
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.Nickname, &item.Content, &item.Type, &params, &item.Status, &item.Remark, &item.CreateTime); err != nil {
			return Page[NotifyTemplate]{}, err
		}
		item.Params = decodeParams(params)
		list = append(list, item)
	}
	return Page[NotifyTemplate]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) NotifySimple(ctx context.Context) ([]NotifyTemplate, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code FROM system_notify_template WHERE deleted=0 AND status=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []NotifyTemplate
	for rows.Next() {
		var item NotifyTemplate
		if err := rows.Scan(&item.ID, &item.Name, &item.Code); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) InsertMessage(ctx context.Context, tenantID int64, item NotifyMessage, params string) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_notify_message
		(user_id, user_type, template_id, template_code, template_nickname, template_content, template_type, template_params, read_status, tenant_id, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 0, ?)`,
		item.UserID, item.UserType, item.TemplateID, item.TemplateCode, item.TemplateNickname, item.TemplateContent, item.TemplateType, params, tenantID, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) MessageByID(ctx context.Context, id int64) (*NotifyMessage, error) {
	return scanMessage(m.DB.QueryRowContext(ctx, messageSelect+` WHERE id=? AND deleted=0`, id))
}

const messageSelect = `SELECT id, user_id, user_type, template_id, template_code, IFNULL(template_nickname,''), IFNULL(template_content,''), template_type, read_status+0, UNIX_TIMESTAMP(read_time)*1000, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_notify_message`

func scanMessage(row *sql.Row) (*NotifyMessage, error) {
	var item NotifyMessage
	var read int
	var readAt sql.NullInt64
	err := row.Scan(&item.ID, &item.UserID, &item.UserType, &item.TemplateID, &item.TemplateCode, &item.TemplateNickname, &item.TemplateContent, &item.TemplateType, &read, &readAt, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.ReadStatus = read == 1
	if readAt.Valid {
		item.ReadTime = &readAt.Int64
	}
	return &item, nil
}

func (m *MySQL) MessagePage(ctx context.Context, tenantID int64, pageNo, pageSize int, userID int64) (Page[NotifyMessage], error) {
	where := `WHERE deleted=0 AND tenant_id=?`
	args := []any{tenantID}
	if userID != 0 {
		where += ` AND user_id=?`
		args = append(args, userID)
	}
	return messagePage(ctx, m.DB, where, args, pageNo, pageSize)
}

func (m *MySQL) MyPage(ctx context.Context, tenantID, userID int64, pageNo, pageSize int, read *bool) (Page[NotifyMessage], error) {
	where := `WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=?`
	args := []any{tenantID, userID, userAdmin}
	if read != nil {
		flag := 0
		if *read {
			flag = 1
		}
		where += ` AND read_status=?`
		args = append(args, flag)
	}
	return messagePage(ctx, m.DB, where, args, pageNo, pageSize)
}

func messagePage(ctx context.Context, db *sql.DB, where string, args []any, pageNo, pageSize int) (Page[NotifyMessage], error) {
	var total int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_notify_message `+where, args...).Scan(&total); err != nil {
		return Page[NotifyMessage]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := db.QueryContext(ctx, messageSelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[NotifyMessage]{}, err
	}
	defer rows.Close()
	list := make([]NotifyMessage, 0)
	for rows.Next() {
		var item NotifyMessage
		var read int
		var readAt sql.NullInt64
		if err := rows.Scan(&item.ID, &item.UserID, &item.UserType, &item.TemplateID, &item.TemplateCode, &item.TemplateNickname, &item.TemplateContent, &item.TemplateType, &read, &readAt, &item.CreateTime); err != nil {
			return Page[NotifyMessage]{}, err
		}
		item.ReadStatus = read == 1
		if readAt.Valid {
			item.ReadTime = &readAt.Int64
		}
		list = append(list, item)
	}
	return Page[NotifyMessage]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) Unread(ctx context.Context, tenantID, userID int64, size int) ([]NotifyMessage, error) {
	if size <= 0 {
		size = 10
	}
	page, err := m.MyPage(ctx, tenantID, userID, 1, size, boolPtr(false))
	return page.List, err
}

func (m *MySQL) UnreadCount(ctx context.Context, tenantID, userID int64) (int64, error) {
	var n int64
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_notify_message WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=? AND read_status=0`, tenantID, userID, userAdmin).Scan(&n)
	return n, err
}

func (m *MySQL) MarkRead(ctx context.Context, tenantID, userID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	holders := strings.Repeat("?,", len(ids))
	holders = holders[:len(holders)-1]
	args := []any{time.Now(), tenantID, userID, userAdmin}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE system_notify_message SET read_status=1, read_time=? WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=? AND id IN (`+holders+`)`, args...)
	return err
}

func (m *MySQL) MarkAllRead(ctx context.Context, tenantID, userID int64) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_notify_message SET read_status=1, read_time=? WHERE deleted=0 AND tenant_id=? AND user_id=? AND user_type=? AND read_status=0`, time.Now(), tenantID, userID, userAdmin)
	return err
}

func (m *MySQL) AccountByID(ctx context.Context, id int64) (*MailAccount, error) {
	var item MailAccount
	var ssl, start int
	err := m.DB.QueryRowContext(ctx, `SELECT id, mail, IFNULL(username,''), IFNULL(password,''), IFNULL(host,''), port, ssl_enable+0, starttls_enable+0 FROM system_mail_account WHERE id=? AND deleted=0`, id).
		Scan(&item.ID, &item.Mail, &item.Username, &item.Password, &item.Host, &item.Port, &ssl, &start)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.SSLEnable = ssl == 1
	item.StartTLSEnable = start == 1
	return &item, nil
}

func (m *MySQL) AccountTemplateCount(ctx context.Context, id int64) (int, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_mail_template WHERE deleted=0 AND account_id=?`, id).Scan(&n)
	return n, err
}

func (m *MySQL) CreateAccount(ctx context.Context, item MailAccount) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_mail_account (mail, username, password, host, port, ssl_enable, starttls_enable, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Mail, item.Username, item.Password, item.Host, item.Port, bit(item.SSLEnable), bit(item.StartTLSEnable), time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateAccount(ctx context.Context, item MailAccount) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_mail_account SET mail=?, username=?, password=?, host=?, port=?, ssl_enable=?, starttls_enable=? WHERE id=? AND deleted=0`,
		item.Mail, item.Username, item.Password, item.Host, item.Port, bit(item.SSLEnable), bit(item.StartTLSEnable), item.ID)
	return err
}

func (m *MySQL) DeleteAccount(ctx context.Context, id int64) error {
	return m.deleteIDs(ctx, "system_mail_account", []int64{id})
}

func (m *MySQL) DeleteAccountList(ctx context.Context, ids []int64) error {
	return m.deleteIDs(ctx, "system_mail_account", ids)
}

func (m *MySQL) AccountPage(ctx context.Context, pageNo, pageSize int, mail string) (Page[MailAccount], error) {
	where := `WHERE deleted=0`
	var args []any
	if mail != "" {
		where += ` AND mail LIKE ?`
		args = append(args, "%"+mail+"%")
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_mail_account `+where, args...).Scan(&total); err != nil {
		return Page[MailAccount]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, mail, IFNULL(username,''), IFNULL(password,''), IFNULL(host,''), port, ssl_enable+0, starttls_enable+0, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_mail_account `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[MailAccount]{}, err
	}
	defer rows.Close()
	list := make([]MailAccount, 0)
	for rows.Next() {
		var item MailAccount
		var ssl, start int
		if err := rows.Scan(&item.ID, &item.Mail, &item.Username, &item.Password, &item.Host, &item.Port, &ssl, &start, &item.CreateTime); err != nil {
			return Page[MailAccount]{}, err
		}
		item.SSLEnable = ssl == 1
		item.StartTLSEnable = start == 1
		list = append(list, item)
	}
	return Page[MailAccount]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) AccountSimple(ctx context.Context) ([]MailAccount, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, mail FROM system_mail_account WHERE deleted=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []MailAccount
	for rows.Next() {
		var item MailAccount
		if err := rows.Scan(&item.ID, &item.Mail); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) MailByID(ctx context.Context, id int64) (*MailTemplate, error) {
	return scanMail(m.DB.QueryRowContext(ctx, mailSelect+` WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) MailByCode(ctx context.Context, code string) (*MailTemplate, error) {
	return scanMail(m.DB.QueryRowContext(ctx, mailSelect+` WHERE code=? AND deleted=0`, code))
}

const mailSelect = `SELECT id, name, code, account_id, IFNULL(nickname,''), title, content, IFNULL(params,'[]'), status, IFNULL(remark,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_mail_template`

func scanMail(row *sql.Row) (*MailTemplate, error) {
	var item MailTemplate
	var params string
	err := row.Scan(&item.ID, &item.Name, &item.Code, &item.AccountID, &item.Nickname, &item.Title, &item.Content, &params, &item.Status, &item.Remark, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Params = decodeParams(params)
	return &item, nil
}

func (m *MySQL) MailCodeTaken(ctx context.Context, code string, exceptID int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_mail_template WHERE deleted=0 AND code=? AND id<>?`, code, exceptID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateMail(ctx context.Context, item MailTemplate) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_mail_template (name, code, account_id, nickname, title, content, params, status, remark, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Name, item.Code, item.AccountID, item.Nickname, item.Title, item.Content, encodeParams(item.Params), item.Status, item.Remark, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateMail(ctx context.Context, item MailTemplate) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_mail_template SET name=?, code=?, account_id=?, nickname=?, title=?, content=?, params=?, status=?, remark=? WHERE id=? AND deleted=0`,
		item.Name, item.Code, item.AccountID, item.Nickname, item.Title, item.Content, encodeParams(item.Params), item.Status, item.Remark, item.ID)
	return err
}

func (m *MySQL) DeleteMail(ctx context.Context, id int64) error {
	return m.deleteIDs(ctx, "system_mail_template", []int64{id})
}

func (m *MySQL) DeleteMailList(ctx context.Context, ids []int64) error {
	return m.deleteIDs(ctx, "system_mail_template", ids)
}

func (m *MySQL) MailPage(ctx context.Context, pageNo, pageSize int, name, code string, status *int) (Page[MailTemplate], error) {
	where := `WHERE deleted=0`
	var args []any
	if name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+name+"%")
	}
	if code != "" {
		where += ` AND code LIKE ?`
		args = append(args, "%"+code+"%")
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_mail_template `+where, args...).Scan(&total); err != nil {
		return Page[MailTemplate]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, mailSelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[MailTemplate]{}, err
	}
	defer rows.Close()
	list := make([]MailTemplate, 0)
	for rows.Next() {
		var item MailTemplate
		var params string
		if err := rows.Scan(&item.ID, &item.Name, &item.Code, &item.AccountID, &item.Nickname, &item.Title, &item.Content, &params, &item.Status, &item.Remark, &item.CreateTime); err != nil {
			return Page[MailTemplate]{}, err
		}
		item.Params = decodeParams(params)
		list = append(list, item)
	}
	return Page[MailTemplate]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) MailSimple(ctx context.Context) ([]MailTemplate, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code FROM system_mail_template WHERE deleted=0 AND status=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []MailTemplate
	for rows.Next() {
		var item MailTemplate
		if err := rows.Scan(&item.ID, &item.Name, &item.Code); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) InsertMailLog(ctx context.Context, account MailAccount, template MailTemplate, to []string, title, content, params string, status int) (int64, error) {
	rawTo, _ := json.Marshal(to)
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_mail_log
		(user_id, user_type, to_mails, account_id, from_mail, template_id, template_code, template_nickname, template_title, template_content, template_params, send_status, deleted, create_time)
		VALUES (0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		userAdmin, string(rawTo), account.ID, account.Mail, template.ID, template.Code, template.Nickname, title, content, params, status, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) FinishMailLog(ctx context.Context, id int64, status int, errText string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_mail_log SET send_status=?, send_time=?, send_exception=? WHERE id=?`, status, time.Now(), cut(errText, 4000), id)
	return err
}

func (m *MySQL) MailLogPage(ctx context.Context, pageNo, pageSize int, toMail string, status *int) (Page[map[string]any], error) {
	where := `WHERE deleted=0`
	var args []any
	if toMail != "" {
		where += ` AND to_mails LIKE ?`
		args = append(args, "%"+toMail+"%")
	}
	if status != nil {
		where += ` AND send_status=?`
		args = append(args, *status)
	}
	return mapPage(ctx, m.DB, `SELECT COUNT(*) FROM system_mail_log `+where, `SELECT id, to_mails, from_mail, template_title, send_status, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_mail_log `+where+` ORDER BY id DESC`, args, pageNo, pageSize, []string{"id", "toMails", "fromMail", "templateTitle", "sendStatus", "createTime"})
}

func (m *MySQL) MailLogByID(ctx context.Context, id int64) (map[string]any, error) {
	var to, from, title, content string
	var status int
	var created int64
	err := m.DB.QueryRowContext(ctx, `SELECT to_mails, from_mail, template_title, IFNULL(template_content,''), send_status, IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_mail_log WHERE id=? AND deleted=0`, id).
		Scan(&to, &from, &title, &content, &status, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "toMails": to, "fromMail": from, "templateTitle": title, "templateContent": content, "sendStatus": status, "createTime": created}, nil
}

func (m *MySQL) ChannelByID(ctx context.Context, id int64) (*SmsChannel, error) {
	var item SmsChannel
	err := m.DB.QueryRowContext(ctx, `SELECT id, signature, code, status, IFNULL(remark,''), IFNULL(api_key,''), IFNULL(api_secret,''), IFNULL(callback_url,'') FROM system_sms_channel WHERE id=? AND deleted=0`, id).
		Scan(&item.ID, &item.Signature, &item.Code, &item.Status, &item.Remark, &item.APIKey, &item.APISecret, &item.CallbackURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &item, err
}

func (m *MySQL) ChannelTemplateCount(ctx context.Context, id int64) (int, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_sms_template WHERE deleted=0 AND channel_id=?`, id).Scan(&n)
	return n, err
}

func (m *MySQL) CreateChannel(ctx context.Context, item SmsChannel) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_sms_channel (signature, code, status, remark, api_key, api_secret, callback_url, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Signature, item.Code, item.Status, item.Remark, item.APIKey, item.APISecret, item.CallbackURL, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateChannel(ctx context.Context, item SmsChannel) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_sms_channel SET signature=?, code=?, status=?, remark=?, api_key=?, api_secret=?, callback_url=? WHERE id=? AND deleted=0`,
		item.Signature, item.Code, item.Status, item.Remark, item.APIKey, item.APISecret, item.CallbackURL, item.ID)
	return err
}

func (m *MySQL) DeleteChannel(ctx context.Context, id int64) error {
	return m.deleteIDs(ctx, "system_sms_channel", []int64{id})
}

func (m *MySQL) DeleteChannelList(ctx context.Context, ids []int64) error {
	return m.deleteIDs(ctx, "system_sms_channel", ids)
}

func (m *MySQL) ChannelPage(ctx context.Context, pageNo, pageSize int, signature string, status *int) (Page[SmsChannel], error) {
	where := `WHERE deleted=0`
	var args []any
	if signature != "" {
		where += ` AND signature LIKE ?`
		args = append(args, "%"+signature+"%")
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_sms_channel `+where, args...).Scan(&total); err != nil {
		return Page[SmsChannel]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, `SELECT id, signature, code, status, IFNULL(remark,''), IFNULL(api_key,''), IFNULL(api_secret,''), IFNULL(callback_url,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_sms_channel `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[SmsChannel]{}, err
	}
	defer rows.Close()
	list := make([]SmsChannel, 0)
	for rows.Next() {
		var item SmsChannel
		if err := rows.Scan(&item.ID, &item.Signature, &item.Code, &item.Status, &item.Remark, &item.APIKey, &item.APISecret, &item.CallbackURL, &item.CreateTime); err != nil {
			return Page[SmsChannel]{}, err
		}
		list = append(list, item)
	}
	return Page[SmsChannel]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) ChannelSimple(ctx context.Context) ([]SmsChannel, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, signature, code FROM system_sms_channel WHERE deleted=0 AND status=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SmsChannel
	for rows.Next() {
		var item SmsChannel
		if err := rows.Scan(&item.ID, &item.Signature, &item.Code); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) SmsByID(ctx context.Context, id int64) (*SmsTemplate, error) {
	return scanSms(m.DB.QueryRowContext(ctx, smsSelect+` WHERE id=? AND deleted=0`, id))
}

func (m *MySQL) SmsByCode(ctx context.Context, code string) (*SmsTemplate, error) {
	return scanSms(m.DB.QueryRowContext(ctx, smsSelect+` WHERE code=? AND deleted=0`, code))
}

const smsSelect = `SELECT id, type, status, code, name, content, IFNULL(params,'[]'), IFNULL(remark,''), IFNULL(api_template_id,''), channel_id, IFNULL(channel_code,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_sms_template`

func scanSms(row *sql.Row) (*SmsTemplate, error) {
	var item SmsTemplate
	var params string
	err := row.Scan(&item.ID, &item.Type, &item.Status, &item.Code, &item.Name, &item.Content, &params, &item.Remark, &item.APITemplateID, &item.ChannelID, &item.ChannelCode, &item.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Params = decodeParams(params)
	return &item, nil
}

func (m *MySQL) SmsCodeTaken(ctx context.Context, code string, exceptID int64) (bool, error) {
	var n int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_sms_template WHERE deleted=0 AND code=? AND id<>?`, code, exceptID).Scan(&n)
	return n > 0, err
}

func (m *MySQL) CreateSms(ctx context.Context, item SmsTemplate) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_sms_template (type, status, code, name, content, params, remark, api_template_id, channel_id, channel_code, deleted, create_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		item.Type, item.Status, item.Code, item.Name, item.Content, encodeParams(item.Params), item.Remark, item.APITemplateID, item.ChannelID, item.ChannelCode, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) UpdateSms(ctx context.Context, item SmsTemplate) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_sms_template SET type=?, status=?, code=?, name=?, content=?, params=?, remark=?, api_template_id=?, channel_id=?, channel_code=? WHERE id=? AND deleted=0`,
		item.Type, item.Status, item.Code, item.Name, item.Content, encodeParams(item.Params), item.Remark, item.APITemplateID, item.ChannelID, item.ChannelCode, item.ID)
	return err
}

func (m *MySQL) DeleteSms(ctx context.Context, id int64) error {
	return m.deleteIDs(ctx, "system_sms_template", []int64{id})
}

func (m *MySQL) DeleteSmsList(ctx context.Context, ids []int64) error {
	return m.deleteIDs(ctx, "system_sms_template", ids)
}

func (m *MySQL) deleteIDs(ctx context.Context, table string, ids []int64) error {
	switch table {
	case "system_notify_template", "system_mail_account", "system_mail_template", "system_sms_channel", "system_sms_template":
	default:
		return &Error{Code: 500, Msg: "系统异常"}
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 1000 {
		return &Error{Code: 400, Msg: "请求参数过多"}
	}
	seen := map[int64]struct{}{}
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	marks := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id
	}
	_, err := m.DB.ExecContext(ctx, `UPDATE `+table+` SET deleted=1 WHERE deleted=0 AND id IN (`+marks+`)`, args...)
	return err
}

func (m *MySQL) SmsPage(ctx context.Context, pageNo, pageSize int, code string, channelID int64, status *int) (Page[SmsTemplate], error) {
	where := `WHERE deleted=0`
	var args []any
	if code != "" {
		where += ` AND code LIKE ?`
		args = append(args, "%"+code+"%")
	}
	if channelID != 0 {
		where += ` AND channel_id=?`
		args = append(args, channelID)
	}
	if status != nil {
		where += ` AND status=?`
		args = append(args, *status)
	}
	var total int64
	if err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_sms_template `+where, args...).Scan(&total); err != nil {
		return Page[SmsTemplate]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(args, pageSize, (pageNo-1)*pageSize)
	rows, err := m.DB.QueryContext(ctx, smsSelect+` `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[SmsTemplate]{}, err
	}
	defer rows.Close()
	list := make([]SmsTemplate, 0)
	for rows.Next() {
		var item SmsTemplate
		var params string
		if err := rows.Scan(&item.ID, &item.Type, &item.Status, &item.Code, &item.Name, &item.Content, &params, &item.Remark, &item.APITemplateID, &item.ChannelID, &item.ChannelCode, &item.CreateTime); err != nil {
			return Page[SmsTemplate]{}, err
		}
		item.Params = decodeParams(params)
		list = append(list, item)
	}
	return Page[SmsTemplate]{List: list, Total: total}, rows.Err()
}

func (m *MySQL) SmsSimple(ctx context.Context) ([]SmsTemplate, error) {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, name, code FROM system_sms_template WHERE deleted=0 AND status=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SmsTemplate
	for rows.Next() {
		var item SmsTemplate
		if err := rows.Scan(&item.ID, &item.Name, &item.Code); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (m *MySQL) InsertSmsLog(ctx context.Context, channel SmsChannel, template SmsTemplate, mobile, content, params string, status int, userID int64, userType int) (int64, error) {
	res, err := m.DB.ExecContext(ctx, `INSERT INTO system_sms_log
		(channel_id, channel_code, template_id, template_code, template_type, template_content, template_params, api_template_id, mobile, user_id, user_type, send_status, receive_status, deleted, create_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)`,
		channel.ID, channel.Code, template.ID, template.Code, template.Type, content, params, template.APITemplateID, mobile, userID, userType, status, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *MySQL) FinishSmsLog(ctx context.Context, id int64, status int, apiCode, apiMsg, serial string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE system_sms_log SET send_status=?, send_time=?, api_send_code=?, api_send_msg=?, api_serial_no=? WHERE id=?`,
		status, time.Now(), cut(apiCode, 63), cut(apiMsg, 255), cut(serial, 255), id)
	return err
}

func (m *MySQL) SmsLogPage(ctx context.Context, pageNo, pageSize int, mobile string, status *int) (Page[map[string]any], error) {
	where := `WHERE deleted=0`
	var args []any
	if mobile != "" {
		where += ` AND mobile LIKE ?`
		args = append(args, "%"+mobile+"%")
	}
	if status != nil {
		where += ` AND send_status=?`
		args = append(args, *status)
	}
	return mapPage(ctx, m.DB, `SELECT COUNT(*) FROM system_sms_log `+where, `SELECT id, mobile, template_code, send_status, IFNULL(api_send_msg,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_sms_log `+where+` ORDER BY id DESC`, args, pageNo, pageSize, []string{"id", "mobile", "templateCode", "sendStatus", "apiSendMsg", "createTime"})
}

func (m *MySQL) SmsLogByID(ctx context.Context, id int64) (map[string]any, error) {
	var mobile, code, msg, content string
	var status int
	var created int64
	err := m.DB.QueryRowContext(ctx, `SELECT mobile, template_code, IFNULL(template_content,''), send_status, IFNULL(api_send_msg,''), IFNULL(UNIX_TIMESTAMP(create_time),0)*1000 FROM system_sms_log WHERE id=? AND deleted=0`, id).
		Scan(&mobile, &code, &content, &status, &msg, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "mobile": mobile, "templateCode": code, "templateContent": content, "sendStatus": status, "apiSendMsg": msg, "createTime": created}, nil
}

func mapPage(ctx context.Context, db *sql.DB, countSQL, listSQL string, args []any, pageNo, pageSize int, keys []string) (Page[map[string]any], error) {
	var total int64
	if err := db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return Page[map[string]any]{}, err
	}
	pageNo, pageSize = pageOf(pageNo, pageSize)
	args = append(append([]any{}, args...), pageSize, (pageNo-1)*pageSize)
	rows, err := db.QueryContext(ctx, listSQL+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[map[string]any]{}, err
	}
	defer rows.Close()
	list := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(keys))
		ptrs := make([]any, len(keys))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Page[map[string]any]{}, err
		}
		item := map[string]any{}
		for i, key := range keys {
			item[key] = normalizeSQL(values[i])
		}
		list = append(list, item)
	}
	return Page[map[string]any]{List: list, Total: total}, rows.Err()
}

func normalizeSQL(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func pageOf(pageNo, pageSize int) (int, int) {
	if pageNo <= 0 {
		pageNo = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return pageNo, pageSize
}

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

func boolPtr(v bool) *bool { return &v }

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
