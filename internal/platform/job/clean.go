package job

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	retainDays     = 14
	deleteLimit    = 100
	maxBatchCount  = 32767 // Java 清理服务同样限制最多 Short.MAX_VALUE 个批次。
	releaseTimeout = 5 * time.Second
)

// Loop 启动后先跑一次，之后按间隔重复，直到 ctx 结束。
func Loop(ctx context.Context, every time.Duration, run func(context.Context, time.Time) error) {
	runOnce := func() {
		if err := run(ctx, time.Now()); err != nil {
			slog.Error("定时清理失败", "err", err)
		}
	}
	runOnce()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// Clean 物理删除过期超过 14 天的令牌与日志，每批最多 100 条。
// 所有删除都在持有命名锁的同一个 MySQL 会话上执行，避免多进程重复清理。
func Clean(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	count, err := withExclusiveSession(ctx, db, func(ctx context.Context, conn *sql.Conn) (int, error) {
		return cleanOnConnection(ctx, conn, now)
	})
	if err == nil && count > 0 {
		slog.Info("定时清理完成", "rows", count)
	}
	return count, err
}

// withExclusiveSession 将一次清理限定在单个 MySQL 会话中。锁不会因固定租期到期而
// 在长任务运行中被其他实例抢走；会话异常断开时，MySQL 会自动释放该锁。
func withExclusiveSession(ctx context.Context, db *sql.DB, run func(context.Context, *sql.Conn) (int, error)) (count int, err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	var database sql.NullString
	if err := conn.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&database); err != nil {
		return 0, fmt.Errorf("查询清理任务数据库: %w", err)
	}
	if !database.Valid || database.String == "" {
		return 0, errors.New("清理任务需要在 MySQL DSN 中指定数据库")
	}
	lockName := cleanupLockName(database.String)
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, lockName).Scan(&acquired); err != nil {
		// 获取锁时如果连接中断，服务端是否已授锁无法确定；不可把该会话放回连接池。
		discardConnection(conn)
		return 0, fmt.Errorf("获取清理任务锁: %w", err)
	}
	if !acquired.Valid {
		discardConnection(conn)
		return 0, errors.New("获取清理任务锁返回 NULL")
	}
	if acquired.Int64 == 0 {
		// 其他实例持锁。本轮跳过，下次调度仍可参加竞争。
		return 0, nil
	}
	if acquired.Int64 != 1 {
		discardConnection(conn)
		return 0, fmt.Errorf("获取清理任务锁返回异常值 %d", acquired.Int64)
	}
	defer func() {
		// 即使上层请求已取消，也要尝试在原会话释放锁。
		releaseCtx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
		defer cancel()
		var released sql.NullInt64
		releaseErr := conn.QueryRowContext(releaseCtx, `SELECT RELEASE_LOCK(?)`, lockName).Scan(&released)
		if releaseErr != nil || !released.Valid || released.Int64 != 1 {
			// 释放状态不明时必须丢弃连接，防止带锁会话返回连接池。
			discardConnection(conn)
			if releaseErr == nil {
				releaseErr = fmt.Errorf("释放清理任务锁返回 %v", released)
			}
			err = errors.Join(err, fmt.Errorf("释放清理任务锁: %w", releaseErr))
		}
	}()
	return run(ctx, conn)
}

func cleanupLockName(database string) string {
	// 命名锁属于整个 MySQL 服务。按数据库区分，避免多个部署共用 MySQL 时相互跳过。
	// 哈希后仍低于 MySQL 64 字符的锁名上限。
	digest := sha256.Sum256([]byte(database))
	return "yudao-cloud-go:clean:" + hex.EncodeToString(digest[:16])
}

func discardConnection(conn *sql.Conn) {
	// database/sql 在 Raw 回调返回 ErrBadConn 时丢弃底层连接。
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}

func cleanOnConnection(ctx context.Context, conn *sql.Conn, now time.Time) (int, error) {
	expire := now.AddDate(0, 0, -retainDays)
	total := 0
	for _, query := range []string{
		`DELETE FROM system_oauth2_refresh_token WHERE expires_time < ? LIMIT ?`,
		`DELETE FROM system_oauth2_access_token WHERE expires_time < ? LIMIT ?`,
		`DELETE FROM infra_api_access_log WHERE create_time < ? LIMIT ?`,
		`DELETE FROM infra_api_error_log WHERE create_time < ? LIMIT ?`,
	} {
		n, err := batches(ctx, conn, query, expire)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func batches(ctx context.Context, conn *sql.Conn, query string, expire time.Time) (int, error) {
	return accumulate(deleteLimit, func() (int, error) {
		res, err := conn.ExecContext(ctx, query, expire, deleteLimit)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		return int(n), err
	})
}

// accumulate 按批删除，某次不足一批就停止。
func accumulate(limit int, next func() (int, error)) (int, error) {
	total := 0
	for batch := 0; batch < maxBatchCount; batch++ {
		n, err := next()
		if err != nil {
			return total, err
		}
		total += n
		if n < limit {
			return total, nil
		}
	}
	return total, nil
}
