// Package db 打开 MySQL。第一期只负责连通性，表访问在各业务仓储里。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Open 建立连接池并立刻 Ping。Ping 失败说明配置或网络有问题，调用方应中止启动。
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL: %w", err)
	}
	// 2C2G 上把连接数压小，给系统和 Redis 留内存。
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接 MySQL: %w", err)
	}
	return sqlDB, nil
}
