package file

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

func TestUploadFileAndRequestBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name          string
		file          string
		fileLimit     int64
		requestOffset int64
		unknownLength bool
		wantCode      int
	}{
		{"单文件和请求恰好到上限", "12345", 5, 0, false, 0},
		{"单文件超出一字节", "123456", 5, 0, false, 400},
		{"已知长度请求超出一字节", "12345", 5, -1, false, 400},
		{"未知长度请求超出一字节", "12345", 5, -1, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			body, contentType := uploadBody(t, tc.file)
			store := &memStore{master: &Config{ID: 22, Storage: storageS3, Master: true}}
			backend := &memBackend{}
			svc := &Service{Store: store, Now: fixedNow, Open: func(Config) (ObjectClient, error) { return backend, nil }}
			h := &handler{svc: svc, limits: UploadLimits{MaxFileBytes: tc.fileLimit, MaxRequestBytes: int64(len(body)) + tc.requestOffset}}
			r := gin.New()
			r.POST("/upload", func(c *gin.Context) { h.upload(c, caller{}) })
			req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			if tc.unknownLength {
				req.ContentLength = -1 // 模拟 chunked：只能靠流式上限识别超限。
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			var result httpx.Result
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK || result.Code != tc.wantCode {
				t.Fatalf("HTTP %d, body %s", rec.Code, rec.Body.String())
			}
			if tc.wantCode == 400 {
				if result.Msg != "上传文件过大，请调整后重试" || store.saved.URL != "" {
					t.Fatalf("超限应沿用 Java 错误包体且不能入库: %s, %+v", rec.Body.String(), store.saved)
				}
			} else if backend.path == "" || store.saved.URL == "" {
				t.Fatalf("边界文件应上传成功: %s, %+v", rec.Body.String(), store.saved)
			}
			// 解析较大文件时会落盘；无论成功或失败，都不能留下 multipart 临时文件。
			entries, err := os.ReadDir(os.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("multipart 临时文件未清理: %v", entries)
			}
		})
	}
}

func TestUploadRejectsAdvertisedOversizeBeforeBodyRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &readProbe{}
	h := &handler{limits: UploadLimits{MaxFileBytes: 5, MaxRequestBytes: 10}}
	r := gin.New()
	r.POST("/upload", func(c *gin.Context) { h.upload(c, caller{}) })
	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.Body = io.NopCloser(body)
	req.ContentLength = 11
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":400`)) || body.reads != 0 {
		t.Fatalf("已知超限请求应在读正文前拒绝: HTTP %d, body %s, reads %d", rec.Code, rec.Body.String(), body.reads)
	}
}

func uploadBody(t *testing.T, content string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("directory", "test"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), w.FormDataContentType()
}

type readProbe struct{ reads int }

func (p *readProbe) Read([]byte) (int, error) {
	p.reads++
	return 0, io.EOF
}
