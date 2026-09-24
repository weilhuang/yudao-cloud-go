package file

import (
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/gin-gonic/gin"
)

// decodeURLPath 对齐 Java 的路径解码：百分号要解码，字面量加号仍是加号。
func decodeURLPath(value string) string {
	if value == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(strings.ReplaceAll(value, "+", "%2B"))
	if err != nil {
		return value
	}
	return decoded
}

// writeDownload 对齐 Java FileTypeUtils.writeAttachment 的响应形状。
// Go 的 MIME 识别与 Java Tika 不完全相同；特殊格式仍须用真实样本做对照。
func writeDownload(c *gin.Context, content []byte, name string) {
	contentType := downloadContentType(content, name)
	disposition := "attachment"
	if strings.HasPrefix(contentType, "image/") {
		disposition = "inline"
	}
	encodedName := strings.ReplaceAll(url.PathEscape(name), "'", "%27")
	c.Header("Content-Disposition", disposition+";filename=\""+fallbackFilename(name)+"\";filename*=UTF-8''"+encodedName)
	if strings.Contains(strings.ToLower(contentType), "video") {
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Length", strconv.Itoa(len(content)))
	}
	c.Data(http.StatusOK, contentType, content)
}

func downloadContentType(content []byte, name string) string {
	detected, _, err := mime.ParseMediaType(http.DetectContentType(content))
	if err != nil {
		detected = "application/octet-stream"
	}
	// 纯字节无法认出 Office 等格式时，再参考文件名；不信任数据库记录的 MIME。
	if detected == "application/octet-stream" || detected == "text/plain" {
		if byName, _, err := mime.ParseMediaType(mime.TypeByExtension(strings.ToLower(path.Ext(name)))); err == nil {
			if detected == "application/octet-stream" || strings.HasPrefix(byName, "application/") {
				return byName
			}
		}
	}
	return detected
}

// Java 会在 ASCII 后备文件名里转义引号和反斜杠，并以 _ 替代非 ASCII 字符。
func fallbackFilename(name string) string {
	if name == "" {
		return "download"
	}
	var out strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case r >= 0x20 && r <= 0x7e:
			out.WriteRune(r)
		default:
			for range utf16.Encode([]rune{r}) {
				out.WriteByte('_')
			}
		}
	}
	return out.String()
}
