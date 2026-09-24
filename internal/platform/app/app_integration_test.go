//go:build integration

package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/app"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/nacos"
)

const testEncryptorPassword = "0123456789abcdef"

func TestHealthAndNacosRegistration(t *testing.T) {
	// 镜像首次拉取可能超过启动本身。时限覆盖拉取，避免容器还在下载时上下文先被取消。
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	mysqlC, err := mysql.Run(ctx, "mysql:8.0",
		mysql.WithDatabase("ruoyi-vue-pro"),
		mysql.WithUsername("root"),
		mysql.WithPassword("123456"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })

	redisC, err := redis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(context.Background()) })

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

	dsn, err := mysqlC.ConnectionString(ctx, "charset=utf8mb4", "parseTime=true", "loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	redisAddr, err := redisC.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	nacosHost, err := nacosC.Host(ctx)
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
	if err := createNamespace(ctx, nacosHost, httpPort.Port(), "dev"); err != nil {
		t.Fatal(err)
	}

	monoCtx, monoCancel := context.WithCancel(ctx)
	defer monoCancel()
	mono, err := app.Start(monoCtx, config.Config{
		App:     config.App{Name: "yudao-server"},
		HTTP:    config.HTTP{Addr: "127.0.0.1:0"},
		MySQL:   config.MySQL{DSN: dsn},
		MyBatis: config.MyBatis{EncryptorPassword: testEncryptorPassword},
		Redis:   config.Redis{Addr: redisAddr},
		Nacos:   config.Nacos{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mono.Shutdown(context.Background()) })
	assertHealth(t, mono.URL(), "yudao-server")

	nacosCfg := config.Nacos{
		Enabled:    true,
		Addr:       nacosHost + ":" + httpPort.Port(),
		GrpcPort:   uint64(grpcPort.Int()),
		Namespace:  "dev",
		Group:      "DEFAULT_GROUP",
		RegisterIP: "127.0.0.1",
		Version:    "1.0.0",
		Tag:        "it",
	}
	systemCfg := nacosCfg
	systemCfg.ServiceName = "system-server"
	infraCfg := nacosCfg
	infraCfg.ServiceName = "infra-server"

	sysCtx, sysCancel := context.WithCancel(ctx)
	defer sysCancel()
	system, err := app.Start(sysCtx, config.Config{
		App:     config.App{Name: "system-server"},
		HTTP:    config.HTTP{Addr: "127.0.0.1:0"},
		MySQL:   config.MySQL{DSN: dsn},
		MyBatis: config.MyBatis{EncryptorPassword: testEncryptorPassword},
		Redis:   config.Redis{Addr: redisAddr},
		Nacos:   systemCfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = system.Shutdown(context.Background()) })
	assertHealth(t, system.URL(), "system-server")

	infraCtx, infraCancel := context.WithCancel(ctx)
	defer infraCancel()
	infra, err := app.Start(infraCtx, config.Config{
		App:     config.App{Name: "infra-server"},
		HTTP:    config.HTTP{Addr: "127.0.0.1:0"},
		MySQL:   config.MySQL{DSN: dsn},
		MyBatis: config.MyBatis{EncryptorPassword: testEncryptorPassword},
		Redis:   config.Redis{Addr: redisAddr},
		Nacos:   infraCfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertHealth(t, infra.URL(), "infra-server")

	waitInstance(t, systemCfg, "system-server")
	waitInstance(t, infraCfg, "infra-server")

	if err := infra.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitGone(t, infraCfg, "infra-server")
}

func createNamespace(ctx context.Context, host, port, id string) error {
	form := url.Values{}
	form.Set("customNamespaceId", id)
	form.Set("namespaceName", id)
	// Nacos API 可能返回非 2xx 或 false，必须在继续注册前暴露真实初始化失败。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+host+":"+port+"/nacos/v1/console/namespaces", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "true" {
		return fmt.Errorf("创建 Nacos 命名空间 %q: HTTP %d, body=%q", id, resp.StatusCode, body)
	}
	return nil
}

func assertHealth(t *testing.T, base, name string) {
	t.Helper()
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health %d", resp.StatusCode)
	}
	var body struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 0 || body.Data["name"] != name || body.Data["status"] != "up" {
		t.Fatalf("health %+v", body)
	}
}

func waitInstance(t *testing.T, cfg config.Nacos, service string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		list, err := nacos.SelectHealthy(cfg, service)
		if err == nil {
			for _, item := range list {
				if item.Metadata["version"] == "1.0.0" && item.Metadata["tag"] == "it" {
					return
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Nacos 上没有健康的 %s", service)
}

func waitGone(t *testing.T, cfg config.Nacos, service string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		list, err := nacos.SelectHealthy(cfg, service)
		if err == nil && len(list) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("注销后 %s 仍在 Nacos", service)
}
