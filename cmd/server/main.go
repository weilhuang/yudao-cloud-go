package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/app"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

// 单体入口，对应 Java 的 YudaoServerApplication。Nacos 默认关闭。
func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Getenv("YUDAO_CONFIG"), config.Monolith())
	if err != nil {
		slog.Error("配置无效", "err", err)
		return 1
	}
	ctx, stop := app.NotifyContext()
	defer stop()
	return app.Execute(ctx, cfg)
}

// 给测试一个不监听信号的入口。生产 main 仍走 run。
func execute(ctx context.Context) int {
	cfg, err := config.Load(os.Getenv("YUDAO_CONFIG"), config.Monolith())
	if err != nil {
		slog.Error("配置无效", "err", err)
		return 1
	}
	return app.Execute(ctx, cfg)
}
