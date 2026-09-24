// Package nacos 用官方 SDK 把本进程注册成 Java 集群能发现的实例。
// 业务包不要引用这里。只有模块启动经 app.Start 间接使用。
package nacos

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

// Instance 是发现结果里调用方关心的字段。
type Instance struct {
	IP       string
	Port     uint64
	Metadata map[string]string
}

// Registrar 在进程退出时摘掉注册。注册使用临时实例，进程消失后 Nacos 也会自己摘除。
type Registrar interface {
	Deregister() error
}

// namingClient 是官方命名客户端的子集，单元测试用假实现，避免连真实 Nacos。
type namingClient interface {
	RegisterInstance(param vo.RegisterInstanceParam) (bool, error)
	DeregisterInstance(param vo.DeregisterInstanceParam) (bool, error)
	SelectInstances(param vo.SelectInstancesParam) ([]model.Instance, error)
	// CloseClient 停掉心跳。临时实例只注销、不停心跳时，Nacos 会马上把它加回来。
	CloseClient()
}

type registrar struct {
	client namingClient
	param  vo.RegisterInstanceParam
}

const (
	// SDK 单次请求最长 5 秒；最多再等 5 次连接恢复，启动失败仍有明确上界。
	registerMaxAttempts = 6
	registerRetryDelay  = time.Second
)

// SDK v2.3.5 的 RPC 客户端表是包级普通 map：创建时写入，关闭时无锁删除。
// 所有由本包创建的命名客户端必须在同一把锁下创建和关闭。
var sdkLifecycleMu sync.Mutex

type sdkClient struct {
	namingClient
	dir    string
	closed bool
}

func (c *sdkClient) CloseClient() {
	sdkLifecycleMu.Lock()
	defer sdkLifecycleMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.namingClient.CloseClient()
	if err := os.RemoveAll(c.dir); err != nil {
		slog.Warn("清理 Nacos 缓存目录失败", "err", err)
	}
}

// Register 向 Nacos 注册当前进程。RegisterIP 为空时用 127.0.0.1，只适合本机联调。
func Register(cfg config.Nacos) (Registrar, error) {
	return register(cfg, newSDK)
}

func register(cfg config.Nacos, create func(config.Nacos) (namingClient, error)) (Registrar, error) {
	client, err := create(cfg)
	if err != nil {
		return nil, err
	}
	reg, err := registerWith(client, cfg)
	if err != nil {
		client.CloseClient()
		return nil, err
	}
	return reg, nil
}

func registerWith(client namingClient, cfg config.Nacos) (Registrar, error) {
	return registerWithRetry(client, cfg, registerMaxAttempts, func() { time.Sleep(registerRetryDelay) })
}

func registerWithRetry(client namingClient, cfg config.Nacos, maxAttempts int, pause func()) (Registrar, error) {
	ip := cfg.RegisterIP
	if ip == "" {
		ip = "127.0.0.1"
	}
	meta := map[string]string{"version": cfg.Version}
	if cfg.Tag != "" {
		// tag 对应 Java 的 yudao.env.tag。本地联调时，网关和 Feign 靠它把流量送回本机。
		meta["tag"] = cfg.Tag
	}
	param := vo.RegisterInstanceParam{
		Ip:          ip,
		Port:        cfg.RegisterPort,
		ServiceName: cfg.ServiceName,
		GroupName:   cfg.Group,
		ClusterName: "DEFAULT",
		Weight:      1,
		Enable:      true,
		Healthy:     true,
		Ephemeral:   true,
		Metadata:    meta,
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ok, err := client.RegisterInstance(param)
		if err == nil {
			if !ok {
				return nil, fmt.Errorf("注册 %s 被 Nacos 拒绝", cfg.ServiceName)
			}
			slog.Info("已注册到 Nacos", "service", cfg.ServiceName, "ip", ip, "port", cfg.RegisterPort, "namespace", cfg.Namespace)
			return &registrar{client: client, param: param}, nil
		}
		// SDK 会在收到 UN_REGISTER 或连接进入 UNHEALTHY 时异步重连。
		// 只等待这些明确的瞬时状态；认证、命名空间和其他配置错误立即返回。
		if !transientRegistrationError(err) || attempt == maxAttempts {
			return nil, fmt.Errorf("注册 %s（第 %d 次）: %w", cfg.ServiceName, attempt, err)
		}
		slog.Warn("Nacos 注册连接尚未恢复，稍后重试", "service", cfg.ServiceName, "attempt", attempt, "err", err.Error())
		pause()
	}
	return nil, fmt.Errorf("注册 %s: 未执行注册请求", cfg.ServiceName)
}

func transientRegistrationError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "Connection is unregistered") ||
		strings.Contains(message, "client not connected, current status:UNHEALTHY") ||
		strings.Contains(message, "client not connected, current status:STARTING")
}

func (r *registrar) Deregister() error {
	// 即使 Nacos 不可达，也必须停掉 SDK 重试并回收本客户端的目录。
	defer r.client.CloseClient()
	ok, err := r.client.DeregisterInstance(vo.DeregisterInstanceParam{
		Ip:          r.param.Ip,
		Port:        r.param.Port,
		ServiceName: r.param.ServiceName,
		GroupName:   r.param.GroupName,
		Cluster:     r.param.ClusterName,
		Ephemeral:   r.param.Ephemeral,
	})
	if err != nil {
		return fmt.Errorf("注销 %s: %w", r.param.ServiceName, err)
	}
	if !ok {
		return fmt.Errorf("注销 %s 被 Nacos 拒绝", r.param.ServiceName)
	}
	return nil
}

// SelectHealthy 查出健康实例。GrpcPort 的规则必须和注册时一致。
func SelectHealthy(cfg config.Nacos, service string) ([]Instance, error) {
	return discovery.selectHealthy(cfg, service)
}

// discoveryKey 只包含连接设置。注册用的服务名、实例端口和灰度标签不影响发现连接。
type discoveryKey struct {
	addr, namespace, group, username, password string
	grpcPort                                   uint64
}

func keyFor(cfg config.Nacos) discoveryKey {
	return discoveryKey{cfg.Addr, cfg.Namespace, cfg.Group, cfg.Username, cfg.Password, cfg.GrpcPort}
}

// SDK 创建命名客户端时会启动 UDP push 接收器；v2.3.5 关闭客户端时不会关闭其 UDP socket。
// 因此查询按连接复用客户端，避免每次 RPC 都留下一个阻塞的 UDP goroutine。
type discoveryPool struct {
	mu      sync.Mutex
	clients map[discoveryKey]*discoveryClient
	create  func(config.Nacos) (namingClient, error)
}

type discoveryClient struct {
	mu sync.Mutex
	namingClient
}

var discovery = discoveryPool{
	clients: make(map[discoveryKey]*discoveryClient),
	create:  func(cfg config.Nacos) (namingClient, error) { return newSDK(cfg) },
}

func (p *discoveryPool) selectHealthy(cfg config.Nacos, service string) ([]Instance, error) {
	p.mu.Lock()
	key := keyFor(cfg)
	client := p.clients[key]
	if client == nil {
		raw, err := p.create(cfg)
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		client = &discoveryClient{namingClient: raw}
		p.clients[key] = client
	}
	// 在池锁下占有该连接，防止 CloseDiscovery 抢先关闭；查询时释放池锁，
	// 让不同 Nacos 配置的调用互不阻塞。
	client.mu.Lock()
	p.mu.Unlock()
	defer client.mu.Unlock()
	list, err := client.SelectInstances(vo.SelectInstancesParam{
		ServiceName: service,
		GroupName:   cfg.Group,
		HealthyOnly: true,
	})
	return mapInstances(service, list, err)
}

// CloseDiscovery 在整个进程结束时关闭发现连接并清理缓存目录。
// 单个服务实例退出时不要调用：同进程的其他服务可能还在共用发现连接。
// SDK v2.3.5 的 UDP 接收器不会随 CloseClient 退出，最终由进程退出回收。
func CloseDiscovery() {
	discovery.close()
}

func (p *discoveryPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, client := range p.clients {
		client.mu.Lock()
		client.CloseClient()
		client.mu.Unlock()
		delete(p.clients, key)
	}
}

func mapInstances(service string, list []model.Instance, err error) ([]Instance, error) {
	if err != nil {
		// 官方客户端在一个健康实例都没有时返回 “instance list is empty!”。
		// 这对调用方是正常结果：没有可转发的实例，不是 Nacos 故障。
		if strings.Contains(err.Error(), "instance list is empty") {
			return []Instance{}, nil
		}
		return nil, fmt.Errorf("查询 %s: %w", service, err)
	}
	out := make([]Instance, 0, len(list))
	for _, item := range list {
		out = append(out, Instance{IP: item.Ip, Port: item.Port, Metadata: item.Metadata})
	}
	return out, nil
}

func newSDK(cfg config.Nacos) (namingClient, error) {
	host, port, err := splitHostPort(cfg.Addr)
	if err != nil {
		return nil, err
	}
	grpcPort := cfg.GrpcPort
	if grpcPort == 0 {
		// Nacos 2.x 默认 gRPC 端口是 HTTP 端口加 1000。端口被容器改写时要在配置里显式给出。
		grpcPort = port + 1000
	}
	dir, err := os.MkdirTemp("", "yudao-nacos-")
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 缓存目录: %w", err)
	}
	opts := []constant.ClientOption{
		constant.WithNamespaceId(cfg.Namespace),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		// 发现连接长期复用，最后一个实例下线时必须接受空列表推送。
		constant.WithUpdateCacheWhenEmpty(true),
		constant.WithLogDir(filepath.Join(dir, "log")),
		constant.WithCacheDir(filepath.Join(dir, "cache")),
		constant.WithLogLevel("error"),
	}
	if cfg.Username != "" {
		opts = append(opts, constant.WithUsername(cfg.Username), constant.WithPassword(cfg.Password))
	}
	clientCfg := *constant.NewClientConfig(opts...)
	serverCfg := []constant.ServerConfig{
		*constant.NewServerConfig(host, port,
			constant.WithGrpcPort(grpcPort),
			constant.WithContextPath("/nacos"),
			constant.WithScheme("http"),
		),
	}
	sdkLifecycleMu.Lock()
	raw, err := clients.NewNamingClient(vo.NacosClientParam{
		ClientConfig:  &clientCfg,
		ServerConfigs: serverCfg,
	})
	sdkLifecycleMu.Unlock()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("创建 Nacos 客户端: %w", err)
	}
	return &sdkClient{namingClient: raw, dir: dir}, nil
}

func splitHostPort(addr string) (string, uint64, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("nacos.addr %q 需要 host:port: %w", addr, err)
	}
	port, err := strconv.ParseUint(portText, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("nacos.addr 端口: %w", err)
	}
	return host, port, nil
}
