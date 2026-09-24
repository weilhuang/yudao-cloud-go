package sheet

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestReadXLSXRoundTrip(t *testing.T) {
	data, err := BuildXLSX("用户列表", []string{"登录名称", "部门编号"}, [][]XLSXCell{
		{{Value: "yunai"}, {Value: "1", Numeric: true}},
		{{Value: `=危险<&`}, {Value: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadXLSX(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][0] != "yunai" || rows[1][1] != "1" || rows[2][0] != `=危险<&` {
		t.Fatalf("读回内容不对：%#v", rows)
	}
	if _, err := ReadXLSX([]byte("not-a-zip")); err == nil {
		t.Fatal("非 XLSX 应失败")
	}
}

func TestReadXLSXUsesFirstWorkbookSheet(t *testing.T) {
	generated, err := BuildXLSX("用户", []string{"登录名称"}, [][]XLSXCell{{{Value: "正确用户"}}})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(generated), int64(len(generated)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		name := file.Name
		if name == "xl/worksheets/sheet1.xml" {
			name = "xl/worksheets/sheet2.xml"
		}
		if name == "xl/_rels/workbook.xml.rels" {
			body = []byte(strings.ReplaceAll(string(body), "worksheets/sheet1.xml", "worksheets/sheet2.xml"))
		}
		part, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	decoy, err := writer.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(decoy, `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>错误工作表</t></is></c></row></sheetData></worksheet>`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadXLSX(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][0] != "正确用户" {
		t.Fatalf("没有按工作簿关系读取首张工作表：%#v", rows)
	}
}
