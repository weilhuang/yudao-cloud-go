package main

import (
	"log/slog"
	"os"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/app"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

// 模块入口，对应 Java 的 InfraServerApplication。注册名必须是 infra-server。
func main() {
	cfg, err := config.Load(os.Getenv("YUDAO_CONFIG"), config.Infra())
	if err != nil {
		slog.Error("配置无效", "err", err)
		os.Exit(1)
	}
	ctx, stop := app.NotifyContext()
	defer stop()
	os.Exit(app.Execute(ctx, cfg))
}
