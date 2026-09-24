package message

import "testing"

func TestFormatAndMissingParam(t *testing.T) {
	content := "你好{name}，验证码{code}"
	names := parseParams(content)
	if len(names) != 2 || names[0] != "name" {
		t.Fatal(names)
	}
	if got := missingParam(names, map[string]any{"name": "张三"}); got != "code" {
		t.Fatal(got)
	}
	got := formatContent(content, map[string]any{"name": "张三", "code": 1234})
	if got != "你好张三，验证码1234" {
		t.Fatal(got)
	}
}
