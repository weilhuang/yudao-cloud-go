package main

import (
	"context"
	"testing"
	"time"
)

func TestExecuteExitsWhenMySQLUnreachable(t *testing.T) {
	t.Setenv("YUDAO_MYSQL_DSN", "root:invalid-for-tests@tcp(127.0.0.1:1)/ruoyi-vue-pro?timeout=300ms")
	t.Setenv("YUDAO_MYBATIS_ENCRYPTOR_PASSWORD", "0123456789abcdef")
	t.Setenv("YUDAO_NACOS_ENABLED", "false")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if code := execute(ctx); code != 1 {
		t.Fatalf("exit %d", code)
	}
}
