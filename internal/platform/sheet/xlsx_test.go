package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBuildXLSXIsRealWorkbookAndKeepsTextSafe(t *testing.T) {
	data, err := BuildXLSX("岗位列表", []string{"岗位序号", "岗位名称", "岗位排序"}, [][]XLSXCell{{
		{Value: "9007199254740993"}, {Value: `=HYPERLINK("bad")<&`}, {Value: "12", Numeric: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatal("导出内容不是 XLSX ZIP 二进制")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := make(map[string]string)
	for _, file := range archive.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[file.Name] = string(body)
		if err := xml.Unmarshal(body, new(any)); err != nil {
			t.Fatalf("%s 不是合法 XML：%v", file.Name, err)
		}
	}
	for _, part := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml"} {
		if parts[part] == "" {
			t.Fatalf("缺少 XLSX 部件 %s", part)
		}
	}
	sheet := parts["xl/worksheets/sheet1.xml"]
	for _, text := range []string{`<c r="A2" t="inlineStr"><is><t xml:space="preserve">9007199254740993</t>`, `=HYPERLINK(&#34;bad&#34;)&lt;&amp;`, `<c r="C2"><v>12</v></c>`} {
		if !strings.Contains(sheet, text) {
			t.Fatalf("XLSX 未保留文本/数值边界 %q：%s", text, sheet)
		}
	}
	if strings.Contains(sheet, `<f>`) {
		t.Fatal("用户输入被写成公式")
	}
}

func TestBuildXLSXRejectsInvalidShape(t *testing.T) {
	if _, err := BuildXLSX("岗位列表", nil, nil); err == nil {
		t.Fatal("空表头应被拒绝")
	}
	if _, err := BuildXLSX("岗位列表", []string{"编号", "名称"}, [][]XLSXCell{{{Value: "1"}}}); err == nil {
		t.Fatal("错列数据应被拒绝")
	}
}

func TestWriteXLSXStreamDoesNotSendPartialDownloadOnFailure(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	err := WriteXLSXStream(c, "岗位数据.xls", "岗位列表", []string{"岗位编号"}, func(emit func([]XLSXCell) error) error {
		if err := emit([]XLSXCell{{Value: "1"}}); err != nil {
			return err
		}
		return errors.New("模拟数据库游标在写入后失败")
	})
	if err == nil || w.Body.Len() != 0 || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("生成失败时不得发送半个 XLSX：err=%v header=%q body=%d", err, w.Header().Get("Content-Disposition"), w.Body.Len())
	}
}
