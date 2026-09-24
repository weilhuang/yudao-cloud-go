//go:build integration

package job

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

func TestCleanUsesOneMySQLExecutorAcrossInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"),
		mysql.WithUsername("root"),
		mysql.WithPassword("123456"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dsn, err := container.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	first, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	// 两个独立连接池代表不同进程。持锁期间即使任务超过普通租期也不能被抢占。
	releaseFirst := make(chan struct{})
	firstStarted := make(chan connectionResult, 1)
	firstDone := make(chan error, 1)
	go func() {
		_, runErr := withExclusiveSession(ctx, first, func(ctx context.Context, conn *sql.Conn) (int, error) {
			id, queryErr := connectionID(ctx, conn)
			firstStarted <- connectionResult{id: id, err: queryErr}
			if queryErr != nil {
				return 0, queryErr
			}
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			return 1, nil
		})
		firstDone <- runErr
	}()
	firstID := receiveID(t, ctx, firstStarted)
	callbackRan := false
	count, err := withExclusiveSession(ctx, second, func(context.Context, *sql.Conn) (int, error) {
		callbackRan = true
		return 1, nil
	})
	if err != nil || count != 0 || callbackRan {
		t.Fatalf("第二实例不得运行: rows=%d callback=%v err=%v", count, callbackRan, err)
	}
	var owner sql.NullInt64
	if err := second.QueryRowContext(ctx, `SELECT IS_USED_LOCK(?)`, cleanupLockName("ruoyi-vue-pro")).Scan(&owner); err != nil || !owner.Valid || owner.Int64 != firstID {
		t.Fatalf("竞争失败不得误释放第一实例的锁: owner=%v err=%v", owner, err)
	}
	close(releaseFirst)
	if err := receiveError(t, ctx, firstDone); err != nil {
		t.Fatal(err)
	}
	count, err = withExclusiveSession(ctx, second, func(context.Context, *sql.Conn) (int, error) {
		return 7, nil
	})
	if err != nil || count != 7 {
		t.Fatalf("第一实例释放后第二实例必须能执行: rows=%d err=%v", count, err)
	}

	// 连接崩溃后服务端自动释放锁；旧任务只用旧会话，不能继续执行删除。
	lostStarted := make(chan connectionResult, 1)
	releaseLost := make(chan struct{})
	lostDone := make(chan error, 1)
	go func() {
		_, runErr := withExclusiveSession(ctx, first, func(ctx context.Context, conn *sql.Conn) (int, error) {
			id, queryErr := connectionID(ctx, conn)
			lostStarted <- connectionResult{id: id, err: queryErr}
			if queryErr != nil {
				return 0, queryErr
			}
			select {
			case <-releaseLost:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			_, execErr := conn.ExecContext(ctx, `SELECT 1`)
			return 0, execErr
		})
		lostDone <- runErr
	}()
	lostID := receiveID(t, ctx, lostStarted)
	if _, err := second.ExecContext(ctx, `KILL CONNECTION `+strconv.FormatInt(lostID, 10)); err != nil {
		t.Fatal(err)
	}
	secondStarted := make(chan connectionResult, 1)
	releaseSecond := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		for {
			rows, runErr := withExclusiveSession(ctx, second, func(ctx context.Context, conn *sql.Conn) (int, error) {
				id, queryErr := connectionID(ctx, conn)
				secondStarted <- connectionResult{id: id, err: queryErr}
				if queryErr != nil {
					return 0, queryErr
				}
				select {
				case <-releaseSecond:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
				return 1, nil
			})
			if runErr != nil || rows == 1 || ctx.Err() != nil {
				secondDone <- runErr
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	secondID := receiveID(t, ctx, secondStarted)
	close(releaseLost)
	if err := receiveError(t, ctx, lostDone); err == nil {
		t.Fatal("旧会话断开后，旧任务必须失败")
	}
	if err := first.QueryRowContext(ctx, `SELECT IS_USED_LOCK(?)`, cleanupLockName("ruoyi-vue-pro")).Scan(&owner); err != nil || !owner.Valid || owner.Int64 != secondID {
		t.Fatalf("旧任务释放失败不能删除新持有者的锁: owner=%v err=%v", owner, err)
	}
	close(releaseSecond)
	if err := receiveError(t, ctx, secondDone); err != nil {
		t.Fatal(err)
	}

	for _, table := range []struct {
		name   string
		column string
	}{
		{"system_oauth2_refresh_token", "expires_time"},
		{"system_oauth2_access_token", "expires_time"},
		{"infra_api_access_log", "create_time"},
		{"infra_api_error_log", "create_time"},
	} {
		ddl := fmt.Sprintf("CREATE TABLE %s (id BIGINT PRIMARY KEY, %s DATETIME NOT NULL)", table.name, table.column)
		if _, err := first.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
		insert := fmt.Sprintf("INSERT INTO %s (id, %s) VALUES (1, ?), (2, ?)", table.name, table.column)
		if _, err := first.ExecContext(ctx, insert, time.Now().AddDate(0, 0, -15), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	count, err = Clean(ctx, first, time.Now())
	if err != nil || count != 4 {
		t.Fatalf("实际清理四类旧数据: rows=%d err=%v", count, err)
	}
	for _, table := range []string{"system_oauth2_refresh_token", "system_oauth2_access_token", "infra_api_access_log", "infra_api_error_log"} {
		var remaining int
		if err := first.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&remaining); err != nil || remaining != 1 {
			t.Fatalf("%s 新数据必须保留: remaining=%d err=%v", table, remaining, err)
		}
	}
}

type connectionResult struct {
	id  int64
	err error
}

func connectionID(ctx context.Context, conn *sql.Conn) (int64, error) {
	var id int64
	err := conn.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&id)
	return id, err
}

func receiveID(t *testing.T, ctx context.Context, ch <-chan connectionResult) int64 {
	t.Helper()
	select {
	case result := <-ch:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result.id
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return 0
	}
}

func receiveError(t *testing.T, ctx context.Context, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}
