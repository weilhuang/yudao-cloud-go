package rpc

import (
	"testing"

	"github.com/weilhuang/yudao-cloud-go/internal/platform/nacos"
)

func TestFilterTagDoesNotFallBack(t *testing.T) {
	list := []nacos.Instance{
		{IP: "127.0.0.1", Port: 1, Metadata: map[string]string{"tag": "local"}},
		{IP: "127.0.0.1", Port: 2, Metadata: map[string]string{}},
	}
	if got := filterTag(list, "local"); len(got) != 1 || got[0].Port != 1 {
		t.Fatalf("%+v", got)
	}
	if got := filterTag(list, "missing"); len(got) != 0 {
		t.Fatalf("不应回退到无 tag 实例: %+v", got)
	}
	if got := filterTag(list, ""); len(got) != 1 || got[0].Port != 2 {
		t.Fatalf("空 tag 应避开本地实例: %+v", got)
	}
}
