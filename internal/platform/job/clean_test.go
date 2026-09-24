package job

import (
	"strings"
	"testing"
)

func TestAccumulateStopsOnShortBatch(t *testing.T) {
	left := []int{100, 100, 20}
	n, err := accumulate(deleteLimit, func() (int, error) {
		got := left[0]
		left = left[1:]
		return got, nil
	})
	if err != nil || n != 220 || len(left) != 0 {
		t.Fatal(n, err, left)
	}
	if retainDays != 14 {
		t.Fatal(retainDays)
	}
}

func TestCleanupLockNameIsStableAndDatabaseScoped(t *testing.T) {
	first := cleanupLockName("ruoyi-vue-pro")
	if first != cleanupLockName("ruoyi-vue-pro") || first == cleanupLockName("other-database") {
		t.Fatal("锁名必须稳定且按数据库隔离")
	}
	if len(first) > 64 || !strings.HasPrefix(first, "yudao-cloud-go:clean:") {
		t.Fatalf("锁名不符合 MySQL 限制: %q", first)
	}
}

func TestAccumulateHasJavaBatchBound(t *testing.T) {
	calls := 0
	n, err := accumulate(deleteLimit, func() (int, error) {
		calls++
		return deleteLimit, nil
	})
	if err != nil || calls != maxBatchCount || n != maxBatchCount*deleteLimit {
		t.Fatalf("无限量数据应在 Java 同样的批次上限停止: rows=%d calls=%d err=%v", n, calls, err)
	}
}
