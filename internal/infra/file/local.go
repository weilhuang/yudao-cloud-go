package file

import (
	"context"
	"os"
	"path/filepath"
)

// localClient 把文件写到 basePath 下，访问地址走本服务的下载接口。
type localClient struct {
	id       int64
	basePath string
	domain   string
}

func (c *localClient) Upload(_ context.Context, content []byte, objectPath, _ string) (string, error) {
	target, err := c.resolve(objectPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return "", err
	}
	return downloadURL(c.domain, c.id, objectPath), nil
}

func (c *localClient) Delete(_ context.Context, objectPath string) error {
	target, err := c.resolve(objectPath)
	if err != nil {
		return err
	}
	err = os.Remove(target)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *localClient) Get(_ context.Context, objectPath string) ([]byte, error) {
	target, err := c.resolve(objectPath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return content, err
}

func (c *localClient) PresignPut(context.Context, string) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *localClient) PresignGet(context.Context, string, int) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *localClient) resolve(objectPath string) (string, error) {
	if !validRelative(objectPath) {
		return "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	base, err := filepath.Abs(c.basePath)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(objectPath)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || stringsHasDotDot(rel) {
		return "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	return target, nil
}

func stringsHasDotDot(rel string) bool {
	return rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}
