package redismonitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

type fakeReader struct {
	info, stats string
	size        int64
	err         error
	calls       int
}

func (f *fakeReader) Info(_ context.Context, section string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	if section == "commandstats" {
		return f.stats, nil
	}
	return f.info, nil
}

func (f *fakeReader) DBSize(context.Context) (int64, error) {
	f.calls++
	return f.size, f.err
}

func TestReadMatchesVbenContractAndRedactsMetadata(t *testing.T) {
	reader := &fakeReader{
		info: "# Server\r\nredis_version:7.2.0\r\nredis_mode:standalone\r\nused_memory_human:1.2M\r\n" +
			"config_file:/secret/redis.conf\r\nmaster_auth:top-secret\r\ncustom_token:abc\r\nmaster_host:internal.host\r\n",
		stats: "# Commandstats\r\ncmdstat_get:calls=12,usec=34,usec_per_call=2.83\r\n" +
			"cmdstat_auth:calls=3,usec=4,usec_per_call=1.33\r\n",
		size: 2,
	}
	got, err := Read(context.Background(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if got.DBSize != 2 || got.Info["redis_version"] != "7.2.0" || got.Info["used_memory_human"] != "1.2M" || len(got.CommandStats) != 2 {
		t.Fatalf("Vben 监控字段不完整: %+v", got)
	}
	if got.Info["config_file"] != "" || got.Info["master_auth"] != "" || got.Info["custom_token"] != "" || got.Info["master_host"] != "" {
		t.Fatalf("敏感信息泄露: %+v", got.Info)
	}
	if got.CommandStats[0] != (CommandStat{Command: "auth", Calls: 3, Usec: 4}) ||
		got.CommandStats[1] != (CommandStat{Command: "get", Calls: 12, Usec: 34}) {
		t.Fatalf("命令统计应保留 AUTH 调用次数并按名称稳定排序: %+v", got.CommandStats)
	}
	if reader.calls != 3 {
		t.Fatalf("应读取 INFO、DBSIZE、INFO commandstats，各一次：%d", reader.calls)
	}
}

func TestReadFailsClosedWhenRedisUnavailableOrStatsMalformed(t *testing.T) {
	for _, reader := range []*fakeReader{
		{err: errors.New("password=do-not-expose")},
		{stats: "cmdstat_get:calls=invalid,usec=3"},
	} {
		if _, err := Read(context.Background(), reader); err == nil {
			t.Fatal("Redis 异常或错误统计不应返回伪成功")
		}
	}
}

type fakeUsers struct{ auth.UserStore }

func (fakeUsers) FindByID(context.Context, int64) (*auth.User, error) {
	return &auth.User{ID: 7, TenantID: 1, Username: "admin", Status: 0}, nil
}

type fakeTokens struct{ auth.TokenStore }

func (fakeTokens) FindAccess(_ context.Context, access string) (*auth.Token, error) {
	if access != "valid" {
		return nil, nil
	}
	return &auth.Token{AccessToken: access, UserID: 7, UserType: 2, TenantID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (fakeTokens) FindRefresh(context.Context, string) (*auth.Token, error) { return nil, nil }

type fakePermissions struct {
	auth.PermissionStore
	allowed bool
}

func (fakePermissions) RolesByUser(context.Context, int64) ([]auth.Role, error) {
	return []auth.Role{{ID: 1, Code: "ops", Status: 0}}, nil
}

func (p fakePermissions) MenusByRole(context.Context, []int64, bool) ([]auth.Menu, error) {
	if !p.allowed {
		return nil, nil
	}
	return []auth.Menu{{ID: 1, Permission: monitorPermission, Type: 3, Status: 0}}, nil
}

func TestMonitorRouteRejectsMissingTenantOtherTenantAndMissingPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, token, tenant string
		allowed             bool
		wantCode            int
	}{
		{name: "未登录", tenant: "1", allowed: true, wantCode: 401},
		{name: "缺少租户", token: "valid", allowed: true, wantCode: 400},
		{name: "跨租户", token: "valid", tenant: "2", allowed: true, wantCode: 403},
		{name: "缺少权限", token: "valid", tenant: "1", wantCode: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{}
			r := gin.New()
			Mount(r, &auth.Service{Users: fakeUsers{}, Tokens: fakeTokens{}, Permissions: fakePermissions{allowed: tc.allowed}}, reader)
			result, _ := request(t, r, tc.token, tc.tenant)
			if result.Code != tc.wantCode || reader.calls != 0 {
				t.Fatalf("拒绝请求仍访问 Redis 或错误码不符: %+v, calls=%d", result, reader.calls)
			}
		})
	}
}

func TestMonitorRouteSuccessAndGenericFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "成功", want: 0},
		{name: "Redis 错误不泄露", err: errors.New("password=do-not-expose"), want: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeReader{info: "redis_version:7.2.0\n", stats: "cmdstat_get:calls=2,usec=3\n", size: 5, err: tc.err}
			r := gin.New()
			Mount(r, &auth.Service{Users: fakeUsers{}, Tokens: fakeTokens{}, Permissions: fakePermissions{allowed: true}}, reader)
			result, raw := request(t, r, "valid", "1")
			if result.Code != tc.want || reader.calls == 0 {
				t.Fatalf("监控请求失败: %+v, calls=%d", result, reader.calls)
			}
			if tc.err != nil {
				if result.Msg != "系统异常" || strings.Contains(raw, "password") {
					t.Fatalf("Redis 错误泄露: %s", raw)
				}
			} else if result.Data["dbSize"] != float64(5) || result.Data["info"].(map[string]any)["redis_version"] != "7.2.0" {
				t.Fatalf("Vben JSON 契约不符: %+v", result.Data)
			}
		})
	}
}

type response struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data map[string]any `json:"data"`
}

func request(t *testing.T, r *gin.Engine, token, tenant string) (response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin-api/infra/redis/get-monitor-info", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenant != "" {
		req.Header.Set("tenant-id", tenant)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var result response
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatal("HTTP " + strconv.Itoa(rec.Code) + ": " + rec.Body.String())
	}
	return result, rec.Body.String()
}
