//go:build integration

package message

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"

	_ "github.com/go-sql-driver/mysql"
)

func TestSmsCallbackMySQL(t *testing.T) {
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
	if _, err := db.Exec(smsCallbackSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQL{DB: db}
	if err := store.ReceiveSms(ctx, "ALIYUN", `[{"success":true,"err_code":"DELIVERED","err_msg":"ok","biz_id":"biz","out_id":"1","report_time":"2024-01-02 03:04:05"}]`); err != nil {
		t.Fatal(err)
	}
	var status int
	var code string
	if err := db.QueryRow(`SELECT receive_status, api_receive_code FROM system_sms_log WHERE id=1`).Scan(&status, &code); err != nil || status != 10 || code != "DELIVERED" {
		t.Fatalf("阿里云回执未写入: %d %s %v", status, code, err)
	}
	if err := store.ReceiveSms(ctx, "TENCENT", `[{"report_status":"FAIL","errmsg":"ERR","description":"失败","sid":"sid-2","user_receive_time":"2024-01-02 04:00:00"}]`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT receive_status FROM system_sms_log WHERE id=2`).Scan(&status); err != nil || status != 20 {
		t.Fatalf("腾讯云应按流水号更新: %d %v", status, err)
	}
	if err := store.ReceiveSms(ctx, "DEBUG", `[]`); err == nil || err.(*Error).Msg != "短信客户端(DEBUG) 不存在" {
		t.Fatal(err)
	}
}

const smsCallbackSchema = `
CREATE TABLE system_sms_channel (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  code varchar(63) NOT NULL,
  status tinyint NOT NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_sms_channel (code, status, deleted) VALUES ('ALIYUN', 0, 0), ('TENCENT', 0, 0);
CREATE TABLE system_sms_log (
  id bigint PRIMARY KEY AUTO_INCREMENT,
  api_serial_no varchar(255) NULL,
  receive_status tinyint NOT NULL DEFAULT 0,
  receive_time datetime NULL,
  api_receive_code varchar(63) NULL,
  api_receive_msg varchar(255) NULL,
  deleted bit(1) NOT NULL DEFAULT 0
);
INSERT INTO system_sms_log (id, api_serial_no, receive_status, deleted) VALUES
  (1, '', 0, 0),
  (2, 'sid-2', 0, 0);
`
