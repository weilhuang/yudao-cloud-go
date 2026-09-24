package sheet

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// ReadXLSX 读取第一个工作表。支持本包写出的 inlineStr，也支持 Excel 常见的共享字符串。
// 经典 BIFF .xls 不是 ZIP，调用方应把它当成无法解析的文件。
func ReadXLSX(raw []byte) ([][]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("不是 XLSX 文件")
	}
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		files[file.Name] = file
	}
	shared, err := readSharedStrings(files)
	if err != nil {
		return nil, err
	}
	sheetName, err := firstSheetPath(files)
	if err != nil {
		return nil, err
	}
	sheet := files[sheetName]
	if sheet == nil {
		return nil, fmt.Errorf("XLSX 缺少工作表")
	}
	body, err := readZipFile(sheet)
	if err != nil {
		return nil, err
	}
	return parseSheet(body, shared)
}

// firstSheetPath 按 workbook 的显示顺序和关系 ID 定位工作表，不能假设首张表叫 sheet1.xml。
func firstSheetPath(files map[string]*zip.File) (string, error) {
	book := files["xl/workbook.xml"]
	rels := files["xl/_rels/workbook.xml.rels"]
	if book == nil || rels == nil {
		return "", fmt.Errorf("XLSX 缺少工作簿关系")
	}
	bookBody, err := readZipFile(book)
	if err != nil {
		return "", err
	}
	var workbook struct {
		Sheets []struct {
			RelationID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(bookBody, &workbook); err != nil {
		return "", fmt.Errorf("XLSX 工作簿无效: %w", err)
	}
	if len(workbook.Sheets) == 0 || workbook.Sheets[0].RelationID == "" {
		return "", fmt.Errorf("XLSX 缺少工作表关系")
	}
	relsBody, err := readZipFile(rels)
	if err != nil {
		return "", err
	}
	var relationships struct {
		Items []struct {
			ID         string `xml:"Id,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
			Type       string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(relsBody, &relationships); err != nil {
		return "", fmt.Errorf("XLSX 工作表关系无效: %w", err)
	}
	for _, rel := range relationships.Items {
		if rel.ID != workbook.Sheets[0].RelationID {
			continue
		}
		if rel.TargetMode == "External" || !strings.HasSuffix(rel.Type, "/worksheet") || rel.Target == "" {
			return "", fmt.Errorf("XLSX 首张工作表关系无效")
		}
		target := path.Clean(path.Join("xl", rel.Target))
		if strings.HasPrefix(rel.Target, "/") {
			target = path.Clean(strings.TrimPrefix(rel.Target, "/"))
		}
		if !strings.HasPrefix(target, "xl/worksheets/") || files[target] == nil {
			return "", fmt.Errorf("XLSX 首张工作表不存在")
		}
		return target, nil
	}
	return "", fmt.Errorf("XLSX 缺少首张工作表关系")
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	const maxPartSize = 32 << 20
	body, err := io.ReadAll(io.LimitReader(reader, maxPartSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPartSize {
		return nil, fmt.Errorf("XLSX 部件超过 32 MiB 限制")
	}
	return body, nil
}

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	file := files["xl/sharedStrings.xml"]
	if file == nil {
		return nil, nil
	}
	body, err := readZipFile(file)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	var out []string
	var buf strings.Builder
	inSI := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch node := tok.(type) {
		case xml.StartElement:
			if node.Name.Local == "si" {
				inSI = true
				buf.Reset()
			}
		case xml.EndElement:
			if node.Name.Local == "si" {
				out = append(out, buf.String())
				inSI = false
			}
		case xml.CharData:
			if inSI {
				buf.Write(node)
			}
		}
	}
	return out, nil
}

func parseSheet(body []byte, shared []string) ([][]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var rows [][]string
	var row []string
	inRow := false
	cellType := ""
	cellRef := ""
	var text strings.Builder
	inV := false
	inT := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch node := tok.(type) {
		case xml.StartElement:
			switch node.Name.Local {
			case "row":
				inRow = true
				row = nil
			case "c":
				cellType = ""
				cellRef = ""
				text.Reset()
				for _, attr := range node.Attr {
					switch attr.Name.Local {
					case "t":
						cellType = attr.Value
					case "r":
						cellRef = attr.Value
					}
				}
			case "v":
				inV = true
				text.Reset()
			case "t":
				if cellType == "inlineStr" {
					inT = true
					text.Reset()
				}
			}
		case xml.CharData:
			if inV || inT {
				text.Write(node)
			}
		case xml.EndElement:
			switch node.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "c":
				value := text.String()
				if cellType == "s" {
					index, err := strconv.Atoi(strings.TrimSpace(value))
					if err != nil || index < 0 || index >= len(shared) {
						return nil, fmt.Errorf("共享字符串下标无效")
					}
					value = shared[index]
				}
				col := columnIndex(cellRef)
				for len(row) < col {
					row = append(row, "")
				}
				if col == len(row) {
					row = append(row, value)
				} else if col >= 0 {
					row[col] = value
				}
				text.Reset()
				cellType = ""
			case "row":
				if inRow {
					rows = append(rows, row)
				}
				inRow = false
			}
		}
	}
	return rows, nil
}

func columnIndex(ref string) int {
	n := 0
	for _, ch := range ref {
		if ch < 'A' || ch > 'Z' && (ch < 'a' || ch > 'z') {
			break
		}
		if ch >= 'a' {
			ch -= 'a' - 'A'
		}
		n = n*26 + int(ch-'A'+1)
	}
	return n - 1
}
