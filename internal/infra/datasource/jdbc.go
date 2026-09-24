package datasource

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// PingMySQL 把 jdbc:mysql URL 转成 Go 驱动能用的 DSN，并在短超时内确认能连通。
// 连不上时返回业务错误，不能把不可用的数据源写入配置表。
func PingMySQL(ctx context.Context, jdbcURL, username, password string) error {
	dsn, err := mysqlDSN(jdbcURL, username, password)
	if err != nil {
		return &Error{Code: codeNotOK, Msg: "数据源配置不正确，无法进行连接"}
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return &Error{Code: codeNotOK, Msg: "数据源配置不正确，无法进行连接"}
	}
	defer db.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return &Error{Code: codeNotOK, Msg: "数据源配置不正确，无法进行连接"}
	}
	return nil
}

func mysqlDSN(jdbcURL, username, password string) (string, error) {
	raw := strings.TrimSpace(jdbcURL)
	raw = strings.TrimPrefix(raw, "jdbc:")
	if !strings.HasPrefix(raw, "mysql://") {
		return "", fmt.Errorf("仅校验 MySQL 数据源")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("缺少主机")
	}
	port := parsed.Port()
	if port == "" {
		port = "3306"
	}
	dbName := strings.TrimPrefix(parsed.Path, "/")
	if dbName == "" {
		return "", fmt.Errorf("缺少库名")
	}
	cfg := mysqldriver.Config{
		User:                 username,
		Passwd:               password,
		Net:                  "tcp",
		Addr:                 net.JoinHostPort(host, port),
		DBName:               dbName,
		ParseTime:            true,
		AllowNativePasswords: true,
		Timeout:              3 * time.Second,
	}
	return cfg.FormatDSN(), nil
}

// OpenMySQL 用 jdbc:mysql 地址打开一个短超时连接。调用方负责关闭。
func OpenMySQL(jdbcURL, username, password string) (*sql.DB, error) {
	dsn, err := mysqlDSN(jdbcURL, username, password)
	if err != nil {
		return nil, err
	}
	return sql.Open("mysql", dsn)
}

// MasterFromDSN 用当前进程的主库拼出 Java 列表里的 id=0。密码不放进返回值。
func MasterFromDSN(dsn string) Item {
	item := Item{ID: idMaster, Name: "master"}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return item
	}
	item.URL = "jdbc:mysql://" + cfg.Addr + "/" + cfg.DBName
	item.Username = cfg.User
	return item
}
