package file

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDecodeURLPathKeepsPlus(t *testing.T) {
	if got := decodeURLPath("a+b%E4%B8%AD.txt"); got != "a+b中.txt" {
		t.Fatal(got)
	}
}

func TestDownloadUsesImageTypeAndEncodedName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// PNG 签名足够让标准库识别图片；文件记录里的 MIME 故意设成错误值。
	content := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 0}
	store := &memStore{
		byID:   &Config{ID: 4, Storage: storageLocal, Config: []byte(`{"basePath":"/tmp","domain":"http://127.0.0.1"}`)},
		byPath: &Item{Name: "学生.png", Type: "application/vnd.ms-excel"},
	}
	svc := &Service{Store: store, Open: func(Config) (ObjectClient, error) {
		return &memBackend{content: content}, nil
	}}
	r := gin.New()
	Mount(r, nil, svc, UploadLimits{})
	req := httptest.NewRequest(http.MethodGet, "/admin-api/infra/file/4/get/2026/a%2Bb.txt", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != string(content) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
	want := "inline;filename=\"__.png\";filename*=UTF-8''" + url.PathEscape("学生.png")
	if rec.Header().Get("Content-Disposition") != want {
		t.Fatal(rec.Header().Get("Content-Disposition"))
	}
}

func TestDownloadVideoHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	content := []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writeDownload(c, content, "a+b.mp4")
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatal(got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;filename=\"a+b.mp4\";") {
		t.Fatal(got)
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" || rec.Header().Get("Content-Length") != "16" {
		t.Fatal(rec.Header())
	}
}

func TestDownloadHeaderEscapesUnsafeName(t *testing.T) {
	if got := fallbackFilename("a\"中\\\n"); got != "a\\\"_\\\\_" {
		t.Fatal(got)
	}
}

func TestDownloadDecodesRawPathOnlyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	backend := &memBackend{content: []byte("ok")}
	store := &memStore{byID: &Config{ID: 4, Storage: storageLocal, Config: []byte(`{"basePath":"/tmp","domain":"http://127.0.0.1"}`)}}
	svc := &Service{Store: store, Open: func(Config) (ObjectClient, error) { return backend, nil }}
	r := gin.New()
	Mount(r, nil, svc, UploadLimits{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin-api/infra/file/4/get/a%252Fb.txt", nil))
	if rec.Code != http.StatusOK || backend.gotGet != "a%2Fb.txt" {
		t.Fatal(rec.Code, backend.gotGet)
	}
}
