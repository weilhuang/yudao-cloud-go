// Package sheet 把列表导出成 Excel 能直接打开的表格。
// 管理后台的导出按钮只把响应存成 .xls，这里用带 BOM 的 CSV，避免再引入表格库。
package sheet

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Write 输出表格。filename 用前端原来的中文名，例如「登录日志.xls」。
func Write(c *gin.Context, filename string, header []string, rows [][]string) {
	c.Header("Content-Type", "application/vnd.ms-excel;charset=UTF-8")
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return
	}
	w := csv.NewWriter(c.Writer)
	_ = w.Write(header)
	for _, row := range rows {
		_ = w.Write(row)
	}
	w.Flush()
}

// Cell 把数字写成表格单元格。
func Cell(n int64) string {
	return strconv.FormatInt(n, 10)
}

// Text 去掉换行，避免一条日志把表格撑成多行。
func Text(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
