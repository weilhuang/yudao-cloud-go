package nacos

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

type fakeNaming struct {
	registered     []vo.RegisterInstanceParam
	registerCalls  int
	registerErrors []error
	removed        []vo.DeregisterInstanceParam
	fail           bool
	deregErr       error
	closed         int
	selected       int
	list           []model.Instance
	selectHook     func()
}

func (f *fakeNaming) RegisterInstance(param vo.RegisterInstanceParam) (bool, error) {
	f.registerCalls++
	if len(f.registerErrors) != 0 {
		err := f.registerErrors[0]
		f.registerErrors = f.registerErrors[1:]
		return false, err
	}
	if f.fail {
		return false, nil
	}
	f.registered = append(f.registered, param)
	return true, nil
}

func (f *fakeNaming) DeregisterInstance(param vo.DeregisterInstanceParam) (bool, error) {
	f.removed = append(f.removed, param)
	if f.deregErr != nil {
		return false, f.deregErr
	}
	return true, nil
}

func (f *fakeNaming) SelectInstances(vo.SelectInstancesParam) ([]model.Instance, error) {
	f.selected++
	if f.selectHook != nil {
		f.selectHook()
	}
	return f.list, nil
}

func (f *fakeNaming) CloseClient() { f.closed++ }

func TestRegisterWritesVersionAndTag(t *testing.T) {
	fake := &fakeNaming{}
	cfg := config.System().Nacos
	cfg.RegisterIP = ""
	cfg.Tag = "dev-weil"
	reg, err := registerWith(fake, cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := fake.registered[0]
	if got.Ip != "127.0.0.1" || got.ServiceName != "system-server" {
		t.Fatalf("param %+v", got)
	}
	if got.Metadata["version"] != "1.0.0" || got.Metadata["tag"] != "dev-weil" {
		t.Fatalf("metadata %+v", got.Metadata)
	}
	if !got.Ephemeral {
		t.Fatal("应注册临时实例，进程退出后由 Nacos 摘除")
	}
	if err := reg.Deregister(); err != nil {
		t.Fatal(err)
	}
	if fake.removed[0].ServiceName != "system-server" || fake.removed[0].Ip != "127.0.0.1" {
		t.Fatalf("deregister %+v", fake.removed[0])
	}
	if fake.closed != 1 {
		t.Fatalf("注销后应关闭客户端，实际 %d 次", fake.closed)
	}
}

func TestEmptyInstanceListIsNotAnError(t *testing.T) {
	list, err := mapInstances("system-server", nil, errString("instance list is empty!"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("len %d", len(list))
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRegisterRejected(t *testing.T) {
	_, err := registerWith(&fakeNaming{fail: true}, config.System().Nacos)
	if err == nil {
		t.Fatal("Nacos 拒绝注册时应返回错误")
	}
}

func TestRegisterRetriesTemporaryConnection(t *testing.T) {
	fake := &fakeNaming{registerErrors: []error{
		errors.New("Connection is unregistered"),
		errors.New("client not connected, current status:UNHEALTHY"),
	}}
	waits := 0
	reg, err := registerWithRetry(fake, config.System().Nacos, 3, func() { waits++ })
	if err != nil {
		t.Fatal(err)
	}
	if fake.registerCalls != 3 || waits != 2 || len(fake.registered) != 1 {
		t.Fatalf("瞬时连接错误应重试至成功: calls=%d waits=%d registered=%d", fake.registerCalls, waits, len(fake.registered))
	}
	if err := reg.Deregister(); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterConfigurationErrorFailsImmediately(t *testing.T) {
	fake := &fakeNaming{registerErrors: []error{errors.New("authorization failed")}}
	waits := 0
	_, err := registerWithRetry(fake, config.System().Nacos, 6, func() { waits++ })
	if err == nil || fake.registerCalls != 1 || waits != 0 {
		t.Fatalf("配置或认证错误不应重试: err=%v calls=%d waits=%d", err, fake.registerCalls, waits)
	}
}

func TestRegisterTemporaryConnectionRetryIsBounded(t *testing.T) {
	fake := &fakeNaming{registerErrors: []error{
		errors.New("client not connected, current status:STARTING"),
		errors.New("client not connected, current status:UNHEALTHY"),
		errors.New("Connection is unregistered"),
	}}
	waits := 0
	_, err := registerWithRetry(fake, config.System().Nacos, 3, func() { waits++ })
	if err == nil || fake.registerCalls != 3 || waits != 2 {
		t.Fatalf("瞬时连接错误重试必须有界: err=%v calls=%d waits=%d", err, fake.registerCalls, waits)
	}
}

func TestRegisterFailureClosesClient(t *testing.T) {
	fake := &fakeNaming{fail: true}
	_, err := register(config.System().Nacos, func(config.Nacos) (namingClient, error) { return fake, nil })
	if err == nil || fake.closed != 1 {
		t.Fatalf("注册失败仍须关闭客户端: err=%v closed=%d", err, fake.closed)
	}
}

func TestDeregisterFailureStillClosesClient(t *testing.T) {
	fake := &fakeNaming{deregErr: errors.New("nacos unavailable")}
	reg, err := registerWith(fake, config.System().Nacos)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Deregister(); err == nil || fake.closed != 1 {
		t.Fatalf("注销失败仍须关闭客户端: err=%v closed=%d", err, fake.closed)
	}
}

func TestDiscoveryPoolReusesClientAndClosesOnce(t *testing.T) {
	var mu sync.Mutex
	var created []*fakeNaming
	pool := discoveryPool{
		clients: make(map[discoveryKey]*discoveryClient),
		create: func(config.Nacos) (namingClient, error) {
			mu.Lock()
			defer mu.Unlock()
			fake := &fakeNaming{list: []model.Instance{{Ip: "127.0.0.1", Port: 48080}}}
			created = append(created, fake)
			return fake, nil
		},
	}
	cfg := config.System().Nacos
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			list, err := pool.selectHealthy(cfg, "system-server")
			if err != nil || len(list) != 1 {
				t.Errorf("发现实例: list=%+v err=%v", list, err)
			}
		}()
	}
	wg.Wait()
	if len(created) != 1 || created[0].selected != 20 {
		t.Fatalf("相同连接应复用一次: created=%d selected=%d", len(created), created[0].selected)
	}
	pool.close()
	if created[0].closed != 1 {
		t.Fatalf("发现客户端应关闭一次: %d", created[0].closed)
	}
	pool.close()
	if created[0].closed != 1 {
		t.Fatalf("重复关闭不应再次关闭客户端: %d", created[0].closed)
	}
}

func TestDiscoveryPoolCloseWaitsForQuery(t *testing.T) {
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	fake := &fakeNaming{selectHook: func() {
		close(queryStarted)
		<-releaseQuery
	}}
	pool := discoveryPool{
		clients: make(map[discoveryKey]*discoveryClient),
		create:  func(config.Nacos) (namingClient, error) { return fake, nil },
	}
	queryDone := make(chan struct{})
	go func() {
		defer close(queryDone)
		_, _ = pool.selectHealthy(config.System().Nacos, "system-server")
	}()
	<-queryStarted
	closeStarted := make(chan struct{})
	closeDone := make(chan struct{})
	go func() {
		close(closeStarted)
		pool.close()
		close(closeDone)
	}()
	<-closeStarted
	select {
	case <-closeDone:
		t.Fatal("进行中的查询不应与关闭并发")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseQuery)
	<-queryDone
	<-closeDone
	if fake.closed != 1 {
		t.Fatalf("查询完成后应关闭客户端: %d", fake.closed)
	}
}

func TestDiscoveryPoolDifferentConnectionsDoNotBlockEachOther(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	slow := &fakeNaming{selectHook: func() {
		close(slowStarted)
		<-releaseSlow
	}}
	fast := &fakeNaming{list: []model.Instance{{Ip: "127.0.0.1", Port: 48080}}}
	pool := discoveryPool{
		clients: make(map[discoveryKey]*discoveryClient),
		create: func(cfg config.Nacos) (namingClient, error) {
			if cfg.Addr == "slow:8848" {
				return slow, nil
			}
			return fast, nil
		},
	}
	slowCfg := config.System().Nacos
	slowCfg.Addr = "slow:8848"
	fastCfg := slowCfg
	fastCfg.Addr = "fast:8848"
	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		_, _ = pool.selectHealthy(slowCfg, "system-server")
	}()
	<-slowStarted
	fastDone := make(chan error, 1)
	go func() {
		list, err := pool.selectHealthy(fastCfg, "system-server")
		if err == nil && len(list) != 1 {
			err = errors.New("健康实例数量错误")
		}
		fastDone <- err
	}()
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(releaseSlow)
		<-slowDone
		t.Fatal("不同连接的查询不应被慢查询阻塞")
	}
	close(releaseSlow)
	<-slowDone
	pool.close()
}

func TestSDKClientRemovesItsOwnTempDir(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "sdk")
	if err := os.Mkdir(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeNaming{}
	client := &sdkClient{namingClient: fake, dir: cache}
	client.CloseClient()
	client.CloseClient()
	if fake.closed != 1 {
		t.Fatalf("SDK 客户端应只关闭一次: %d", fake.closed)
	}
	if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缓存目录应删除: %v", err)
	}
}
