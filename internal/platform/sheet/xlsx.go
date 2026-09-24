package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
)

// XLSXCell 的 Numeric 只供可信的整数列使用；文本列始终写为字符串，
// 防止备注、名称等用户输入被 Excel 当成公式执行。
type XLSXCell struct {
	Value   string
	Numeric bool
}

// WriteXLSXStream 把数据库行逐条编码到隔离临时文件；完整生成后才发送下载头。
// 大批量导出只保留一行数据在内存中，失败时也能返回正常 JSON 错误。
func WriteXLSXStream(c *gin.Context, filename, sheetName string, header []string,
	stream func(emit func([]XLSXCell) error) error) error {
	file, err := os.CreateTemp("", "yudao-export-*.xlsx")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := EncodeXLSX(file, sheetName, header, stream); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	c.Header("Content-Disposition", "attachment;filename="+url.PathEscape(filename))
	c.DataFromReader(http.StatusOK, info.Size(), "application/vnd.ms-excel;charset=UTF-8", file, nil)
	return nil
}

// BuildXLSX 是小数据和单元测试的便利方法；全表导出必须使用 WriteXLSXStream。
// Excel 单表最多 1,048,576 行（含标题），超出时明确返回错误。
func BuildXLSX(sheetName string, header []string, rows [][]XLSXCell) ([]byte, error) {
	var output bytes.Buffer
	err := EncodeXLSX(&output, sheetName, header, func(emit func([]XLSXCell) error) error {
		for _, row := range rows {
			if err := emit(row); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// EncodeXLSX 接受逐行回调，供 MySQL 导出在写临时文件时保持固定内存占用。
func EncodeXLSX(output io.Writer, sheetName string, header []string,
	stream func(emit func([]XLSXCell) error) error) error {
	if len(header) == 0 || len(header) > 16384 {
		return fmt.Errorf("XLSX 表格超出行列限制")
	}
	archive := zip.NewWriter(output)
	parts := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/_rels/workbook.xml.rels"} {
		if err := xlsxPart(archive, name, parts[name]); err != nil {
			return err
		}
	}
	book, err := archive.Create("xl/workbook.xml")
	if err != nil {
		return err
	}
	if _, err = io.WriteString(book, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="`); err != nil {
		return err
	}
	if err = xlsxEscape(book, sheetName); err != nil {
		return err
	}
	if _, err = io.WriteString(book, `" sheetId="1" r:id="rId1"/></sheets></workbook>`); err != nil {
		return err
	}
	sheet, err := archive.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	if _, err = io.WriteString(sheet, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return err
	}
	titles := make([]XLSXCell, len(header))
	for i, title := range header {
		titles[i] = XLSXCell{Value: title}
	}
	if err = xlsxRow(sheet, 1, titles); err != nil {
		return err
	}
	rowNo := 1
	err = stream(func(row []XLSXCell) error {
		rowNo++
		if rowNo > 1048576 {
			return fmt.Errorf("XLSX 表格超出行限制")
		}
		if len(row) != len(header) {
			return fmt.Errorf("XLSX 第 %d 行列数不匹配", rowNo)
		}
		return xlsxRow(sheet, rowNo, row)
	})
	if err != nil {
		return err
	}
	if _, err = io.WriteString(sheet, `</sheetData></worksheet>`); err != nil {
		return err
	}
	if err = archive.Close(); err != nil {
		return err
	}
	return nil
}

func xlsxPart(archive *zip.Writer, name, value string) error {
	part, err := archive.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(part, value)
	return err
}

func xlsxRow(out io.Writer, number int, cells []XLSXCell) error {
	if _, err := fmt.Fprintf(out, `<row r="%d">`, number); err != nil {
		return err
	}
	for i, cell := range cells {
		ref := xlsxColumn(i+1) + fmt.Sprint(number)
		if cell.Numeric {
			if _, err := fmt.Fprintf(out, `<c r="%s"><v>%s</v></c>`, ref, cell.Value); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(out, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">`, ref); err != nil {
			return err
		}
		if err := xlsxEscape(out, cell.Value); err != nil {
			return err
		}
		if _, err := io.WriteString(out, `</t></is></c>`); err != nil {
			return err
		}
	}
	_, err := io.WriteString(out, `</row>`)
	return err
}

func xlsxColumn(number int) string {
	var column string
	for number > 0 {
		number--
		column = string(rune('A'+number%26)) + column
		number /= 26
	}
	return column
}

func xlsxEscape(out io.Writer, value string) error {
	// EscapeText 同时转义引号，可用于文本节点及 sheet name 属性。
	return xml.EscapeText(out, []byte(value))
}
