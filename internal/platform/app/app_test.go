package app

import (
	"context"
	"testing"
	"time"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

const testEncryptorPassword = "0123456789abcdef"

func TestExecuteExitsWhenMySQLUnreachable(t *testing.T) {
	cfg := config.Monolith()
	cfg.MySQL.DSN = "root:invalid-for-tests@tcp(127.0.0.1:1)/ruoyi-vue-pro?timeout=300ms&readTimeout=300ms&writeTimeout=300ms"
	cfg.MyBatis.EncryptorPassword = testEncryptorPassword
	cfg.Nacos.Enabled = false

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if code := Execute(ctx, cfg); code != 1 {
		t.Fatalf("连不上 MySQL 时应退出码 1，实际 %d", code)
	}
}
