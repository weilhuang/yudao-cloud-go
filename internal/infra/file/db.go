package file

import "context"

// BlobStore 是数据库存储器的文件内容。同一路径多次上传时，读取最后一条。
type BlobStore interface {
	SaveBlob(ctx context.Context, configID int64, objectPath string, content []byte) error
	LoadBlob(ctx context.Context, configID int64, objectPath string) ([]byte, error)
	DeleteBlob(ctx context.Context, configID int64, objectPath string) error
}

type dbClient struct {
	id     int64
	domain string
	blobs  BlobStore
}

func (c *dbClient) Upload(ctx context.Context, content []byte, objectPath, _ string) (string, error) {
	if !validRelative(objectPath) {
		return "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	if err := c.blobs.SaveBlob(ctx, c.id, objectPath, content); err != nil {
		return "", err
	}
	return downloadURL(c.domain, c.id, objectPath), nil
}

func (c *dbClient) Delete(ctx context.Context, objectPath string) error {
	return c.blobs.DeleteBlob(ctx, c.id, objectPath)
}

func (c *dbClient) Get(ctx context.Context, objectPath string) ([]byte, error) {
	return c.blobs.LoadBlob(ctx, c.id, objectPath)
}

func (c *dbClient) PresignPut(context.Context, string) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *dbClient) PresignGet(context.Context, string, int) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}
