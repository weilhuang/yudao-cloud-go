package file

import (
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"net/http"
	"path"
	"strings"
)

// completeFileMetadata 对应 Java 上传时可省略名称和 MIME 的约定。
// 标准库对少数格式的识别与 Tika 不完全相同，迁移验收仍须用真实文件样本对照。
func completeFileMetadata(content []byte, name, contentType string) (string, string) {
	if contentType == "" {
		if name != "" {
			contentType = mime.TypeByExtension(strings.ToLower(path.Ext(name)))
		}
		if contentType == "" {
			contentType = http.DetectContentType(content)
		}
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			contentType = mediaType
		}
	}
	if name == "" {
		// uploadPath 随后会按内容生成 SHA-256 名称。
		if suffix := extensionForMIME(contentType); suffix != "" {
			name = digestName(content) + suffix
		}
	} else if path.Ext(name) == "" {
		name += extensionForMIME(contentType)
	}
	return name, contentType
}

func digestName(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func extensionForMIME(contentType string) string {
	// 优先使用 Java Tika 对常见媒体类型采用的扩展名，避免系统 MIME 表中的别名顺序不同。
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	case "application/octet-stream":
		return ".bin"
	}
	return ""
}
