package file

import (
	"strings"
	"testing"
	"time"
)

func TestUploadPathUsesDateDirectory(t *testing.T) {
	name, rel, err := uploadPath("a.png", "avatar", []byte("x"), time.Date(2026, 9, 22, 0, 0, 0, 0, time.Local))
	if err != nil || name != "a.png" || rel != "avatar/20260922/a.png" {
		t.Fatal(name, rel, err)
	}
}

func TestUploadPathRejectsTraversal(t *testing.T) {
	_, _, err := uploadPath("../a.png", "", []byte("x"), time.Now())
	biz, _ := err.(*Error)
	if biz == nil || biz.Code != 1_001_003_003 {
		t.Fatal(err)
	}
}

func TestPublicVisitURLUsesDomain(t *testing.T) {
	cfg := ClientConfig{Domain: "http://test.yudao.iocoder.cn", Endpoint: "s3-cn-south-1.qiniucs.com", Bucket: "ruoyi-vue-pro"}
	got := publicVisitURL(cfg, "20260922/a.png")
	if got != "http://test.yudao.iocoder.cn/20260922/a.png" {
		t.Fatal(got)
	}
}

func TestQiniuRequiresDomain(t *testing.T) {
	_, err := normalizeS3(ClientConfig{Endpoint: "s3-cn-south-1.qiniucs.com", Bucket: "b", AccessKey: "k", AccessSecret: "s"})
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatal(err)
	}
}
