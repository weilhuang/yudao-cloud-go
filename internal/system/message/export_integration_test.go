//go:build integration

package message

import (
	"bytes"
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestSmsTemplateExportOnMySQL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"), mysql.WithUsername("root"), mysql.WithPassword("123456"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })
	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true", "multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`SET time_zone = '+08:00'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`SET time_zone = '+08:00';
CREATE TABLE system_sms_template (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 type tinyint NOT NULL,
 status tinyint NOT NULL,
 code varchar(63) NOT NULL,
 name varchar(63) NOT NULL,
 content varchar(255) NOT NULL,
 remark varchar(255) NULL,
 api_template_id varchar(63) NOT NULL,
 channel_id bigint NOT NULL,
 channel_code varchar(63) NOT NULL,
 create_time datetime NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0
);
CREATE TABLE system_dict_data (
 id bigint PRIMARY KEY AUTO_INCREMENT,
 sort int NOT NULL,
 label varchar(100) NOT NULL,
 value varchar(100) NOT NULL,
 dict_type varchar(100) NOT NULL,
 deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_dict_data (sort, label, value, dict_type) VALUES
 (1, '验证码', '1', 'system_sms_template_type'),
 (1, '开启', '0', 'common_status'),
 (1, '阿里云', 'ALIYUN', 'system_sms_channel_code');
INSERT INTO system_sms_template (type, status, code, name, content, remark, api_template_id, channel_id, channel_code, create_time) VALUES
 (1, 0, 'admin-sms-login', '登录', '验证码{code}', '', 'SMS_1', 10, 'ALIYUN', '2026-09-23 10:00:00'),
 (1, 0, 'other', '其他', '别的', '', 'SMS_2', 10, 'ALIYUN', '2026-09-23 11:00:00');
`); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	var buf bytes.Buffer
	err = sheet.EncodeXLSX(&buf, "数据", []string{"编号", "短信签名", "开启状态", "模板编码", "短信渠道编码", "创建时间"}, func(emit func([]sheet.XLSXCell) error) error {
		types, err := store.DictText(ctx, "system_sms_template_type")
		if err != nil {
			return err
		}
		status, err := store.DictText(ctx, "common_status")
		if err != nil {
			return err
		}
		channels, err := store.DictText(ctx, "system_sms_channel_code")
		if err != nil {
			return err
		}
		return store.SmsExportRows(ctx, SmsExportFilter{Code: "login"}, func(item SmsTemplate) error {
			return emit([]sheet.XLSXCell{
				{Value: strconv.FormatInt(item.ID, 10)},
				{Value: labelInt(types, item.Type)},
				{Value: labelInt(status, item.Status)},
				{Value: item.Code},
				{Value: labelOf(channels, item.ChannelCode)},
				{Value: excelMillisValue(item.CreateTime)},
			})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := sheet.ReadXLSX(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][3] != "admin-sms-login" || rows[1][1] != "验证码" || rows[1][2] != "开启" || rows[1][4] != "阿里云" || rows[1][5] == "" {
		t.Fatalf("%v", rows)
	}
}
