package file

import (
	"net/url"
	"strings"
)

// presignObjectPath 将主文件配置的完整访问地址还原为对象名。
// 只有配置域名下的 URL 会解码并去掉旧签名；裸对象名保持原样。
func presignObjectPath(domain, raw string) (string, error) {
	objectPath := raw
	prefix := strings.TrimRight(domain, "/") + "/"
	if strings.HasPrefix(raw, prefix) {
		objectPath = strings.TrimPrefix(raw, prefix)
		if stop := strings.IndexAny(objectPath, "?#"); stop >= 0 {
			objectPath = objectPath[:stop]
		}
		decoded, err := url.PathUnescape(objectPath)
		if err != nil {
			return "", &Error{Code: 400, Msg: "文件访问地址不正确"}
		}
		objectPath = decoded
	}
	if !validRelative(objectPath) {
		return "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	return objectPath, nil
}
