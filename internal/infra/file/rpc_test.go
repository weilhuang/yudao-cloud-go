package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func TestFileRPCCreateMatchesByteArrayAndAnonymousBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	content := []byte{0, 255, 10}
	store := &memStore{master: &Config{ID: 22, Storage: storageS3}}
	backend := &rpcBackend{}
	r := rpcRouter(store, backend, UploadLimits{})
	body, _ := json.Marshal(rpcCreateFileRequest{Name: "a.bin", Directory: "avatar", Type: "application/octet-stream", Content: content})
	result := rpcRequest(t, r, http.MethodPost, "/rpc-api/infra/file/create", body)
	if result.Code != 0 || result.Data != "https://cdn.example.com/saved" {
		t.Fatalf("未登录 Feign 创建应返回 CommonResult<String>: %+v", result)
	}
	if !bytes.Equal(backend.content, content) || backend.path != "avatar/20260922/a.bin" || backend.contentType != "application/octet-stream" || store.saved.Size != 3 {
		t.Fatalf("Base64 内容或存储记录错误: backend=%+v saved=%+v", backend, store.saved)
	}
	// RPC 与管理端同名路径分离；调用 RPC 不应要求浏览器令牌或 tenant-id。
	if got := rpcRequest(t, r, http.MethodPost, "/admin-api/infra/file/create", body); got.Code == 0 {
		t.Fatalf("RPC 不能意外注册为管理端记录创建接口: %+v", got)
	}
}

func TestFileRPCCreateValidationAndLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		body   []byte
		limits UploadLimits
		code   int
		msg    string
	}{
		{"空内容", []byte(`{"content":""}`), UploadLimits{}, 400, "请求参数不正确:文件内容不能为空"},
		{"非法 Base64", []byte(`{"content":"!"}`), UploadLimits{}, 400, "请求参数不正确"},
		{"目录穿越", []byte(`{"directory":"../bad","content":"eA=="}`), UploadLimits{}, 1_001_003_003, "文件路径不正确"},
		{"文件超过上限", []byte(`{"content":"eHl6"}`), UploadLimits{MaxFileBytes: 2, MaxRequestBytes: 128}, 400, "上传文件过大，请调整后重试"},
		{"请求体超过上限", []byte(`{"content":"eHl6"}`), UploadLimits{MaxFileBytes: 2, MaxRequestBytes: 8}, 400, "上传文件过大，请调整后重试"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memStore{master: &Config{ID: 22, Storage: storageS3}}
			backend := &rpcBackend{}
			r := rpcRouter(store, backend, tc.limits)
			got := rpcRequest(t, r, http.MethodPost, "/rpc-api/infra/file/create", tc.body)
			if got.Code != tc.code || got.Msg != tc.msg || backend.path != "" || store.saved.URL != "" {
				t.Fatalf("错误请求不应触发上传或入库: %+v backend=%+v saved=%+v", got, backend, store.saved)
			}
		})
	}
}

func TestFileRPCCreateCompletesMissingMetadataAndPropagatesFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	content := []byte("png")
	store := &memStore{master: &Config{ID: 22, Storage: storageS3}}
	backend := &rpcBackend{}
	r := rpcRouter(store, backend, UploadLimits{})
	result := rpcRequest(t, r, http.MethodPost, "/rpc-api/infra/file/create", []byte(`{"type":"image/png","content":"cG5n"}`))
	sum := sha256.Sum256(content)
	wantName := hex.EncodeToString(sum[:]) + ".png"
	if result.Code != 0 || store.saved.Name != wantName || store.saved.Type != "image/png" || backend.path != "20260922/"+wantName {
		t.Fatalf("省略名称时应按内容摘要及 MIME 补扩展名: %+v saved=%+v backend=%+v", result, store.saved, backend)
	}
	backend.failure = errors.New("对象存储失败")
	result = rpcRequest(t, r, http.MethodPost, "/rpc-api/infra/file/create", []byte(`{"name":"b.png","type":"image/png","content":"eA=="}`))
	if result.Code != 500 || result.Msg != "系统异常" || store.saved.Name != wantName {
		t.Fatalf("存储失败须隐藏内部细节且不新增记录: %+v saved=%+v", result, store.saved)
	}
}

func TestFileRPCPresignGetNormalizesFullURLAndExpiration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memStore{master: rpcS3Config(false)}
	backend := &rpcBackend{}
	r := rpcRouter(store, backend, UploadLimits{})
	raw := "https://cdn.example.com/20260923/%E4%B8%AD%E6%96%87%2B.png?old=signature#fragment"
	result := rpcRequest(t, r, http.MethodGet, "/rpc-api/infra/file/presigned-url?url="+url.QueryEscape(raw)+"&expirationSeconds=60", nil)
	if result.Code != 0 || result.Data != "https://signed.example.com/get" || backend.presignPath != "20260923/中文+.png" || backend.presignSeconds != 60 {
		t.Fatalf("GET 签名应从完整 URL 还原对象名并传递秒数: %+v backend=%+v", result, backend)
	}
	result = rpcRequest(t, r, http.MethodGet, "/rpc-api/infra/file/presigned-url?url=raw/path.png", nil)
	if result.Code != 0 || backend.presignPath != "raw/path.png" || backend.presignSeconds != 0 {
		t.Fatalf("裸对象名兼容与缺省 24h 标记错误: %+v backend=%+v", result, backend)
	}
}

func TestFileRPCPresignGetValidationAndStorageBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		cfg  *Config
		url  string
		code int
	}{
		{"缺少 URL", rpcS3Config(false), "/rpc-api/infra/file/presigned-url", 400},
		{"过期秒数无效", rpcS3Config(false), "/rpc-api/infra/file/presigned-url?url=a.png&expirationSeconds=abc", 400},
		{"过期秒数超出 Java Integer", rpcS3Config(false), "/rpc-api/infra/file/presigned-url?url=a.png&expirationSeconds=2147483648", 400},
		{"私有桶零秒", rpcS3Config(false), "/rpc-api/infra/file/presigned-url?url=a.png&expirationSeconds=0", 400},
		{"路径穿越", rpcS3Config(false), "/rpc-api/infra/file/presigned-url?url=..%2Fsecret", 1_001_003_003},
		{"外部域名", rpcS3Config(false), "/rpc-api/infra/file/presigned-url?url=https%3A%2F%2Fevil.example%2Fa.png", 1_001_003_003},
		{"非 S3 主存储器", &Config{ID: 1, Storage: storageDB}, "/rpc-api/infra/file/presigned-url?url=a.png", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &rpcBackend{}
			r := rpcRouter(&memStore{master: tc.cfg}, backend, UploadLimits{})
			if tc.cfg.Storage == storageDB {
				backend.presignError = &Error{Code: 400, Msg: "当前存储器不支持预签名"}
			}
			got := rpcRequest(t, r, http.MethodGet, tc.url, nil)
			if got.Code != tc.code {
				t.Fatalf("错误业务码不符: %+v", got)
			}
			if got.Code != 0 && backend.presignPath != "" && tc.cfg.Storage != storageDB {
				t.Fatalf("错误路径不能调用存储器: %+v", backend)
			}
		})
	}
}

func TestFileRPCPresignGetUsesS3DefaultAndExplicitSeconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := rpcS3Config(false)
	// 用真实 S3 presigner 验证缺省 24 小时与显式秒数，不发网络请求。
	svc := &Service{Store: &memStore{master: cfg}}
	for _, tc := range []struct {
		seconds *int
		want    string
	}{
		{nil, "86400"},
		{intPtr(90), "90"},
	} {
		got, err := svc.PresignGetURL(context.Background(), "https://cdn.example.com/a.png", tc.seconds)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(got)
		if err != nil || parsed.Query().Get("X-Amz-Expires") != tc.want {
			t.Fatalf("签名时效错误: %q, %v", got, err)
		}
	}
	store := &memStore{master: rpcS3Config(true)}
	svc.Store = store
	got, err := svc.PresignGetURL(context.Background(), "https://cdn.example.com/a.png", intPtr(0))
	if err != nil || got != "https://cdn.example.com/a.png" {
		t.Fatalf("公开桶无需签名，应忽略秒数: %q, %v", got, err)
	}
}

func rpcS3Config(public bool) *Config {
	return &Config{ID: 22, Storage: storageS3, Master: true, Config: mustJSON(ClientConfig{
		Endpoint: "https://s3.example.com", Bucket: "demo", Domain: "https://cdn.example.com",
		AccessKey: "ak", AccessSecret: "sk", EnablePublicAccess: boolPtr(public), EnablePathStyleAccess: true,
	})}
}

func rpcRouter(store *memStore, backend ObjectClient, limits UploadLimits) *gin.Engine {
	r := gin.New()
	svc := &Service{Store: store, Now: fixedNow, Open: func(Config) (ObjectClient, error) { return backend, nil }}
	MountRPC(r, svc, limits)
	return r
}

func rpcRequest(t *testing.T, r *gin.Engine, method, path string, body []byte) httpx.Result {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var got httpx.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		// 未注册的管理端路径是 Gin 的 404 文本，不应被当作成功 RPC。
		if rec.Code == http.StatusNotFound {
			return httpx.Result{Code: http.StatusNotFound}
		}
		t.Fatalf("响应不是 CommonResult: HTTP %d, %s, %v", rec.Code, rec.Body.String(), err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("应采用 Java 风格 HTTP 200 + 业务码: HTTP %d, %+v", rec.Code, got)
	}
	return got
}

type rpcBackend struct {
	content        []byte
	path           string
	contentType    string
	failure        error
	presignPath    string
	presignSeconds int
	presignError   error
}

func (b *rpcBackend) Upload(_ context.Context, content []byte, objectPath, contentType string) (string, error) {
	if b.failure != nil {
		return "", b.failure
	}
	b.content = append([]byte(nil), content...)
	b.path = objectPath
	b.contentType = contentType
	return "https://cdn.example.com/saved", nil
}

func (b *rpcBackend) Delete(context.Context, string) error { return nil }
func (b *rpcBackend) Get(context.Context, string) ([]byte, error) {
	return nil, nil
}
func (b *rpcBackend) PresignPut(context.Context, string) (string, error) {
	return "", errors.New("不应调用 PUT 签名")
}
func (b *rpcBackend) PresignGet(_ context.Context, objectPath string, seconds int) (string, error) {
	b.presignPath = objectPath
	b.presignSeconds = seconds
	if b.presignError != nil {
		return "", b.presignError
	}
	return "https://signed.example.com/get", nil
}

func intPtr(v int) *int { return &v }

func TestPresignObjectPathRejectsMalformedEncoding(t *testing.T) {
	_, err := presignObjectPath("https://cdn.example.com", "https://cdn.example.com/a%ZZ.png")
	if err == nil || !strings.Contains(err.Error(), "文件访问地址") {
		t.Fatal(err)
	}
}
