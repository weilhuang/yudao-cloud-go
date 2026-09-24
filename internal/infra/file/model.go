// Package file 按主文件配置上传。存储器编号与 Java 一致：数据库 1、本地 10、FTP 11、SFTP 12、S3 20。
package file

import "encoding/json"

const (
	storageDB    = 1
	storageLocal = 10
	storageFTP   = 11
	storageSFTP  = 12
	storageS3    = 20
)

// Config 是文件配置。Config 字段保持前端传来的 JSON，里面可以有 Java 的 @class。
type Config struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Storage    int             `json:"storage"`
	Master     bool            `json:"master"`
	Config     json.RawMessage `json:"config"`
	Remark     string          `json:"remark"`
	CreateTime int64           `json:"createTime,omitempty"`
}

// Item 是已上传文件。
type Item struct {
	ID         int64  `json:"id"`
	ConfigID   int64  `json:"configId"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	URL        string `json:"url"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	CreateTime int64  `json:"createTime,omitempty"`
}

// Presign 是前端直传需要的地址。
type Presign struct {
	ConfigID  int64  `json:"configId"`
	Path      string `json:"path"`
	UploadURL string `json:"uploadUrl"`
	URL       string `json:"url"`
}

// Page 是管理后台分页。
type Page[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// Error 是文件接口的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// ClientConfig 覆盖数据库、本地和 S3 的配置字段。多余的 JSON 字段会被忽略。
type ClientConfig struct {
	Domain                string `json:"domain"`
	BasePath              string `json:"basePath"`
	Endpoint              string `json:"endpoint"`
	Bucket                string `json:"bucket"`
	AccessKey             string `json:"accessKey"`
	AccessSecret          string `json:"accessSecret"`
	EnablePathStyleAccess bool   `json:"enablePathStyleAccess"`
	EnablePublicAccess    *bool  `json:"enablePublicAccess"`
	Region                string `json:"region"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	Username              string `json:"username"`
	Password              string `json:"password"`
	Mode                  string `json:"mode"`
}

func parseClient(raw json.RawMessage) (ClientConfig, error) {
	var cfg ClientConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	err := json.Unmarshal(raw, &cfg)
	return cfg, err
}
