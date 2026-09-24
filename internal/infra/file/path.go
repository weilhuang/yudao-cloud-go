package file

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"
)

func uploadPath(name, directory string, content []byte, now time.Time) (string, string, error) {
	if !validDirectory(directory) {
		return "", "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	if name == "" {
		sum := sha256.Sum256(content)
		name = hex.EncodeToString(sum[:])
	}
	if !validFileName(name) {
		return "", "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	rel := now.Format("20060102") + "/" + name
	if directory != "" {
		rel = strings.Trim(directory, "/") + "/" + rel
	}
	if !validRelative(rel) {
		return "", "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	return name, rel, nil
}

func validFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`) && strings.IndexByte(name, 0) < 0
}

func validDirectory(directory string) bool {
	return directory == "" || validRelative(directory)
}

func validRelative(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || strings.ContainsAny(p, `\:`) || strings.IndexByte(p, 0) >= 0 {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func encodeObjectPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func joinDomain(domain, extra string) string {
	return strings.TrimRight(domain, "/") + "/" + strings.TrimLeft(extra, "/")
}

func downloadURL(domain string, configID int64, objectPath string) string {
	return joinDomain(domain, "admin-api/infra/file/"+itoa(configID)+"/get/"+encodeObjectPath(objectPath))
}

func stripQuery(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return strings.Split(raw, "?")[0]
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
