//go:build integration && java_integration

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/app"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/config"
)

// TestJavaGoSharedTokens 只在显式指定冻结 Java JAR 和 SQL 时运行。
// 每次创建全新的 MySQL/Redis 容器；SQL 含 DROP TABLE，绝不能导入已有数据库。
func TestJavaGoSharedTokens(t *testing.T) {
	jarPath := os.Getenv("YUDAO_JAVA_JAR")
	sqlPath := os.Getenv("YUDAO_JAVA_SQL")
	javaHome := os.Getenv("YUDAO_JAVA_HOME")
	if jarPath == "" || sqlPath == "" || javaHome == "" {
		t.Skip("设置 YUDAO_JAVA_JAR、YUDAO_JAVA_SQL、YUDAO_JAVA_HOME 后才运行 Java/Go 混跑测试")
	}
	for _, path := range []string{jarPath, sqlPath, javaHome + "/bin/java"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("混跑测试文件不可读 %s: %v", path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	// WithScripts 仅在新建的 ruoyi-vue-pro 容器数据库初始化时执行冻结 SQL。
	mysqlC, err := mysql.Run(ctx, "mysql:8.0", mysql.WithDatabase("ruoyi-vue-pro"),
		mysql.WithUsername("root"), mysql.WithPassword("123456"), mysql.WithScripts(sqlPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysqlC.Terminate(context.Background()) })
	redisC, err := redis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(context.Background()) })
	dsn, err := mysqlC.ConnectionString(ctx, "charset=utf8mb4", "parseTime=true", "loc=Local")
	if err != nil {
		t.Fatal(err)
	}
	mysqlHost, err := mysqlC.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mysqlPort, err := mysqlC.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatal(err)
	}
	redisHost, err := redisC.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	redisPort, err := redisC.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	redisAddr := net.JoinHostPort(redisHost, redisPort.Port())

	goServer, err := app.Start(ctx, config.Config{
		App: config.App{Name: "yudao-server"}, HTTP: config.HTTP{Addr: "127.0.0.1:0"},
		MySQL: config.MySQL{DSN: dsn}, Redis: config.Redis{Addr: redisAddr},
		MyBatis: config.MyBatis{EncryptorPassword: testEncryptorPassword},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = goServer.Shutdown(context.Background()) })

	// Java 单体禁用 Nacos/OpenFeign，独立端口与 Go 共享新建的 MySQL/Redis。
	javaPort := unusedLocalPort(t)
	jdbc := fmt.Sprintf("jdbc:mysql://%s/ruoyi-vue-pro?useSSL=false&serverTimezone=Asia/Shanghai&allowPublicKeyRetrieval=true",
		net.JoinHostPort(mysqlHost, mysqlPort.Port()))
	javaCtx, stopJava := context.WithCancel(ctx)
	javaCmd := exec.CommandContext(javaCtx, javaHome+"/bin/java", "-Xmx1g", "-jar", jarPath,
		"--spring.profiles.active=local",
		"--server.address=127.0.0.1", "--server.port="+strconv.Itoa(javaPort),
		"--spring.datasource.dynamic.datasource.master.url="+jdbc,
		"--spring.datasource.dynamic.datasource.slave.url="+jdbc,
		"--spring.data.redis.host="+redisHost,
		"--spring.data.redis.port="+redisPort.Port(),
		"--yudao.security.mock-enable=false")
	var javaOutput lockedBuffer
	javaCmd.Stdout = &javaOutput
	javaCmd.Stderr = &javaOutput
	if err := javaCmd.Start(); err != nil {
		stopJava()
		t.Fatal(err)
	}
	javaDone := make(chan struct{})
	var javaWaitErr error
	go func() {
		javaWaitErr = javaCmd.Wait()
		close(javaDone)
	}()
	t.Cleanup(func() {
		stopJava()
		<-javaDone
	})
	javaURL := "http://127.0.0.1:" + strconv.Itoa(javaPort)
	if err := awaitJavaHTTP(ctx, javaURL, javaDone, &javaWaitErr); err != nil {
		stopJava()
		<-javaDone
		t.Fatalf("Java 单体未就绪: %v\nJava 输出末尾:\n%s", err, tail(javaOutput.String(), 4000))
	}

	goLogin := postJSON(t, goServer.URL()+"/admin-api/system/auth/login",
		`{"username":"admin","password":"admin123"}`, "1")
	if goLogin.Code != 0 {
		t.Fatalf("Go 登录失败: %+v", goLogin)
	}
	goToken, _ := goLogin.Data["accessToken"].(string)
	if goToken == "" {
		t.Fatal("Go 未签发访问令牌")
	}
	// Java 必须能读取 Go 写入的 MySQL/Redis 令牌，再构造真实权限信息。
	javaInfo := getAuth(t, javaURL+"/admin-api/system/auth/get-permission-info", goToken, "1")
	if javaInfo.Code != 0 || javaInfo.Data["user"] == nil {
		t.Fatalf("Java 未接受 Go 令牌: %+v\nJava 输出末尾:\n%s", javaInfo, tail(javaOutput.String(), 4000))
	}
	// 用同一 Go 令牌向两端读取冻结 SQL 中的记录，比较新补的详情合同关键字段。
	// 这里只断言确定性的业务字段；时间、空字符串与可选展示字段另由更广的差分集验证。
	for _, sample := range []struct {
		path   string
		fields []string
	}{
		{path: "/admin-api/system/dict-type/get?id=1", fields: []string{"id", "name", "type", "status"}},
		{path: "/admin-api/system/dict-data/get?id=1", fields: []string{"id", "label", "value", "dictType", "sort"}},
		{path: "/admin-api/system/dept/get?id=100", fields: []string{"id", "name", "parentId", "sort", "status"}},
	} {
		javaDetail := getAuth(t, javaURL+sample.path, goToken, "1")
		goDetail := getAuth(t, goServer.URL()+sample.path, goToken, "1")
		if javaDetail.Code != 0 || goDetail.Code != 0 || javaDetail.Data == nil || goDetail.Data == nil {
			t.Fatalf("详情差分失败 %s: Java=%+v Go=%+v", sample.path, javaDetail, goDetail)
		}
		for _, field := range sample.fields {
			if fmt.Sprint(javaDetail.Data[field]) != fmt.Sprint(goDetail.Data[field]) {
				t.Fatalf("详情字段差分失败 %s %s: Java=%v Go=%v", sample.path, field,
					javaDetail.Data[field], goDetail.Data[field])
			}
		}
	}
	for _, path := range []string{
		"/admin-api/system/dict-type/get?id=999999999",
		"/admin-api/system/dict-data/get?id=999999999",
		"/admin-api/system/dept/get?id=999999999",
	} {
		javaDetail := getAuth(t, javaURL+path, goToken, "1")
		goDetail := getAuth(t, goServer.URL()+path, goToken, "1")
		if javaDetail.Code != 0 || goDetail.Code != 0 || javaDetail.Data != nil || goDetail.Data != nil {
			t.Fatalf("缺失详情应返回 null %s: Java=%+v Go=%+v", path, javaDetail, goDetail)
		}
	}
	javaLogin := postJSON(t, javaURL+"/admin-api/system/auth/login",
		`{"username":"admin","password":"admin123"}`, "1")
	if javaLogin.Code != 0 {
		t.Fatalf("Java 登录失败: %+v\nJava 输出末尾:\n%s", javaLogin, tail(javaOutput.String(), 4000))
	}
	javaToken, _ := javaLogin.Data["accessToken"].(string)
	if javaToken == "" {
		t.Fatal("Java 未签发访问令牌")
	}
	goInfo := getAuth(t, goServer.URL()+"/admin-api/system/auth/get-permission-info", javaToken, "1")
	if goInfo.Code != 0 || goInfo.Data["user"] == nil {
		t.Fatalf("Go 未接受 Java 令牌: %+v", goInfo)
	}
	// 刷新使用对方签发的 refreshToken，检查两边数据库与 Redis 的轮换语义。
	goRefresh, _ := goLogin.Data["refreshToken"].(string)
	javaRefreshed := postJSON(t, javaURL+"/admin-api/system/auth/refresh-token?refreshToken="+url.QueryEscape(goRefresh), `{}`, "1")
	if javaRefreshed.Code != 0 {
		t.Fatalf("Java 无法刷新 Go 令牌: %+v", javaRefreshed)
	}
	javaNewAccess, _ := javaRefreshed.Data["accessToken"].(string)
	if checked := getAuth(t, goServer.URL()+"/admin-api/system/auth/get-permission-info", javaNewAccess, "1"); checked.Code != 0 {
		t.Fatalf("Go 无法接受 Java 刷新的访问令牌: %+v", checked)
	}
	javaRefresh, _ := javaLogin.Data["refreshToken"].(string)
	goRefreshed := postJSON(t, goServer.URL()+"/admin-api/system/auth/refresh-token?refreshToken="+url.QueryEscape(javaRefresh), `{}`, "1")
	if goRefreshed.Code != 0 {
		t.Fatalf("Go 无法刷新 Java 令牌: %+v", goRefreshed)
	}
	goNewAccess, _ := goRefreshed.Data["accessToken"].(string)
	if checked := getAuth(t, javaURL+"/admin-api/system/auth/get-permission-info", goNewAccess, "1"); checked.Code != 0 {
		t.Fatalf("Java 无法接受 Go 刷新的访问令牌: %+v", checked)
	}
	// 两边分别注销对方刚签发的令牌，另一边应立刻拒绝旧凭据。
	if code := postLogoutCode(t, javaURL+"/admin-api/system/auth/logout", goNewAccess); code != 0 {
		t.Fatalf("Java 注销 Go 访问令牌失败: %d", code)
	}
	if checked := getAuth(t, goServer.URL()+"/admin-api/system/auth/get-permission-info", goNewAccess, "1"); checked.Code == 0 {
		t.Fatalf("Go 仍接受已由 Java 注销的访问令牌: %+v", checked)
	}
	if code := postLogoutCode(t, goServer.URL()+"/admin-api/system/auth/logout", javaNewAccess); code != 0 {
		t.Fatalf("Go 注销 Java 访问令牌失败: %d", code)
	}
	if checked := getAuth(t, javaURL+"/admin-api/system/auth/get-permission-info", javaNewAccess, "1"); checked.Code == 0 {
		t.Fatalf("Java 仍接受已由 Go 注销的访问令牌: %+v", checked)
	}
}

func postLogoutCode(t *testing.T, endpoint, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("tenant-id", "1")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Code
}

func unusedLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func awaitJavaHTTP(ctx context.Context, base string, done <-chan struct{}, waitErr *error) error {
	client := &http.Client{Timeout: time.Second}
	for {
		select {
		case <-done:
			return fmt.Errorf("Java 进程提前退出: %w", *waitErr)
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/actuator/health", nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// exec.Cmd 的输出协程与测试断言并发读写，互斥保护失败日志快照。
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func tail(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[len(value)-max:])
}
