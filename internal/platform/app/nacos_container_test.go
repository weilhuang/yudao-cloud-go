//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

func startNacos(t *testing.T, ctx context.Context) config.Nacos {
	t.Helper()
	nacosC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "nacos/nacos-server:v2.4.3",
			ExposedPorts: []string{"8848/tcp", "9848/tcp"},
			Env: map[string]string{
				"MODE":              "standalone",
				"NACOS_AUTH_ENABLE": "false",
				"JVM_XMS":           "256m",
				"JVM_XMX":           "256m",
			},
			WaitingFor: nacosReady(),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nacosC.Terminate(context.Background()) })
	host, err := nacosC.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	httpPort, err := nacosC.MappedPort(ctx, "8848/tcp")
	if err != nil {
		t.Fatal(err)
	}
	grpcPort, err := nacosC.MappedPort(ctx, "9848/tcp")
	if err != nil {
		t.Fatal(err)
	}
	if err := createNamespace(ctx, host, httpPort.Port(), "dev"); err != nil {
		t.Fatal(err)
	}
	return config.Nacos{
		Enabled:    true,
		Addr:       host + ":" + httpPort.Port(),
		GrpcPort:   uint64(grpcPort.Int()),
		Namespace:  "dev",
		Group:      "DEFAULT_GROUP",
		RegisterIP: "127.0.0.1",
		Version:    "1.0.0",
	}
}

// 控制台首页和监听端口会先于命名服务开放；注册客户端要等命名服务报告 UP。
func nacosReady() wait.Strategy {
	return wait.ForAll(
		wait.ForListeningPort("8848/tcp"),
		wait.ForListeningPort("9848/tcp"),
		wait.ForHTTP("/nacos/v1/ns/operator/metrics").WithPort("8848/tcp").WithResponseMatcher(func(body io.Reader) bool {
			var metrics struct {
				Status string `json:"status"`
			}
			return json.NewDecoder(body).Decode(&metrics) == nil && metrics.Status == "UP"
		}),
	).WithDeadline(3 * time.Minute)
}
