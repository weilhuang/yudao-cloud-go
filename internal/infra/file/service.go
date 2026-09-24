package file

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ObjectClient 是一种存储器。上传返回浏览器可访问的地址。
type ObjectClient interface {
	Upload(ctx context.Context, content []byte, objectPath, contentType string) (string, error)
	Delete(ctx context.Context, objectPath string) error
	Get(ctx context.Context, objectPath string) ([]byte, error)
	PresignPut(ctx context.Context, objectPath string) (string, error)
	PresignGet(ctx context.Context, objectPath string, seconds int) (string, error)
}

// Store 读写文件记录和文件配置。
type Store interface {
	Master(ctx context.Context) (*Config, error)
	ConfigByID(ctx context.Context, id int64) (*Config, error)
	ConfigPage(ctx context.Context, pageNo, pageSize int, name string, storage *int) (Page[Config], error)
	CreateConfig(ctx context.Context, item Config) (int64, error)
	UpdateConfig(ctx context.Context, item Config) error
	UpdateMaster(ctx context.Context, id int64) error
	DeleteConfig(ctx context.Context, id int64) error

	InsertFile(ctx context.Context, item Item) (int64, error)
	FilePage(ctx context.Context, pageNo, pageSize int, path, mime string) (Page[Item], error)
	FileByID(ctx context.Context, id int64) (*Item, error)
	FileByPath(ctx context.Context, configID int64, objectPath string) (*Item, error)
	DeleteFile(ctx context.Context, id int64) error
}

// Service 上传时读取主配置，不会固定写本地盘。
type Service struct {
	Store Store
	Blobs BlobStore
	Now   func() time.Time
	HTTP  *http.Client
	// Open 测试时替换存储器。生产留空，按 storage 选择数据库、本地或 S3。
	Open func(cfg Config) (ObjectClient, error)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Upload 把内容传到当前主存储器，并记下文件记录。
func (s *Service) Upload(ctx context.Context, content []byte, name, directory, contentType string) (string, error) {
	if len(content) == 0 {
		return "", &Error{Code: 1_001_003_002, Msg: "文件为空"}
	}
	// Java 的 FileService 会在保存前补全可识别的 MIME 和扩展名；RPC 可省略这两项。
	name, contentType = completeFileMetadata(content, name, contentType)
	name, objectPath, err := uploadPath(name, directory, content, s.now())
	if err != nil {
		return "", err
	}
	master, client, err := s.masterClient(ctx)
	if err != nil {
		return "", err
	}
	visit, err := client.Upload(ctx, content, objectPath, contentType)
	if err != nil {
		return "", err
	}
	_, err = s.Store.InsertFile(ctx, Item{ConfigID: master.ID, Name: name, Path: objectPath, URL: visit, Type: contentType, Size: int64(len(content))})
	return visit, err
}

// PresignGetURL 接受 Java FileApi 所用的完整访问 URL，返回用于读取的地址。
// 主存储器不是 S3 时沿用其“不支持预签名”错误；此处不复用上传用的 PresignPut。
func (s *Service) PresignGetURL(ctx context.Context, rawURL string, expirationSeconds *int) (string, error) {
	master, client, err := s.masterClient(ctx)
	if err != nil {
		return "", err
	}
	path := rawURL
	if master.Storage == storageS3 {
		cfg, err := parseClient(master.Config)
		if err != nil {
			return "", err
		}
		cfg, err = normalizeS3(cfg)
		if err != nil {
			return "", err
		}
		path, err = presignObjectPath(cfg.Domain, rawURL)
		if err != nil {
			return "", err
		}
		// Java 只在私有桶实际签名时检查 Duration；公开桶忽略过期参数。
		if cfg.EnablePublicAccess != nil && !*cfg.EnablePublicAccess && expirationSeconds != nil && *expirationSeconds <= 0 {
			return "", &Error{Code: 400, Msg: "访问有效期必须大于 0 秒"}
		}
	}
	seconds := 0 // nil 与 S3 默认的 24 小时相同。
	if expirationSeconds != nil {
		seconds = *expirationSeconds
	}
	return client.PresignGet(ctx, path, seconds)
}

// PresignPut 只对 S3 主配置生成直传地址。
func (s *Service) PresignPut(ctx context.Context, name, directory string) (*Presign, error) {
	safeName, objectPath, err := uploadPath(name, directory, nil, s.now())
	if err != nil {
		return nil, err
	}
	_ = safeName
	master, client, err := s.masterClient(ctx)
	if err != nil {
		return nil, err
	}
	uploadURL, err := client.PresignPut(ctx, objectPath)
	if err != nil {
		return nil, err
	}
	visit, err := client.PresignGet(ctx, objectPath, 0)
	if err != nil {
		return nil, err
	}
	return &Presign{ConfigID: master.ID, Path: objectPath, UploadURL: uploadURL, URL: visit}, nil
}

// CreateRecord 记录前端已经直传完成的文件。访问地址去掉签名参数。
func (s *Service) CreateRecord(ctx context.Context, item Item) (int64, error) {
	if !validRelative(item.Path) || !validFileName(item.Name) {
		return 0, &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	item.URL = stripQuery(item.URL)
	return s.Store.InsertFile(ctx, item)
}

// Delete 先删存储器里的对象，再删记录。
func (s *Service) Delete(ctx context.Context, id int64) error {
	item, err := s.Store.FileByID(ctx, id)
	if err != nil {
		return err
	}
	if item == nil {
		return &Error{Code: 1_001_003_001, Msg: "文件不存在"}
	}
	if !validRelative(item.Path) {
		return &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	client, err := s.clientByID(ctx, item.ConfigID)
	if err != nil {
		return err
	}
	if err := client.Delete(ctx, item.Path); err != nil {
		return err
	}
	return s.Store.DeleteFile(ctx, id)
}

// Content 给公开下载接口读取文件字节。
func (s *Service) Content(ctx context.Context, configID int64, objectPath string) ([]byte, *Item, error) {
	if !validRelative(objectPath) {
		return nil, nil, &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	client, err := s.clientByID(ctx, configID)
	if err != nil {
		return nil, nil, err
	}
	content, err := client.Get(ctx, objectPath)
	if err != nil {
		return nil, nil, err
	}
	item, err := s.Store.FileByPath(ctx, configID, objectPath)
	return content, item, err
}

// SaveConfig 创建或修改配置。七牛必须带自定义域名。
func (s *Service) SaveConfig(ctx context.Context, item Config) (int64, error) {
	if item.Name == "" {
		return 0, &Error{Code: 400, Msg: "配置名不能为空"}
	}
	if err := validateStorage(item); err != nil {
		return 0, err
	}
	if item.ID == 0 {
		return s.Store.CreateConfig(ctx, item)
	}
	current, err := s.Store.ConfigByID(ctx, item.ID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, &Error{Code: 1_001_006_000, Msg: "文件配置不存在"}
	}
	return item.ID, s.Store.UpdateConfig(ctx, item)
}

// DeleteConfig 主配置不能删，否则后面无法上传。
func (s *Service) DeleteConfig(ctx context.Context, id int64) error {
	current, err := s.Store.ConfigByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: 1_001_006_000, Msg: "文件配置不存在"}
	}
	if current.Master {
		return &Error{Code: 1_001_006_001, Msg: "该文件配置不允许删除，原因：它是主配置，删除会导致无法上传文件"}
	}
	return s.Store.DeleteConfig(ctx, id)
}

// Test 往指定配置传一段文本，用来确认存储器能写。
func (s *Service) Test(ctx context.Context, id int64) (string, error) {
	cfg, err := s.Store.ConfigByID(ctx, id)
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", &Error{Code: 1_001_006_000, Msg: "文件配置不存在"}
	}
	client, err := s.open(*cfg)
	if err != nil {
		return "", err
	}
	_, objectPath, err := uploadPath("yudao.txt", "test", []byte("yudao"), s.now())
	if err != nil {
		return "", err
	}
	return client.Upload(ctx, []byte("yudao"), objectPath, "text/plain")
}

func (s *Service) masterClient(ctx context.Context) (*Config, ObjectClient, error) {
	master, err := s.Store.Master(ctx)
	if err != nil {
		return nil, nil, err
	}
	if master == nil {
		return nil, nil, &Error{Code: 1_001_006_000, Msg: "文件配置不存在"}
	}
	client, err := s.open(*master)
	return master, client, err
}

func (s *Service) clientByID(ctx context.Context, id int64) (ObjectClient, error) {
	cfg, err := s.Store.ConfigByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, &Error{Code: 1_001_006_000, Msg: "文件配置不存在"}
	}
	return s.open(*cfg)
}

func (s *Service) open(cfg Config) (ObjectClient, error) {
	if s.Open != nil {
		return s.Open(cfg)
	}
	clientCfg, err := parseClient(cfg.Config)
	if err != nil {
		return nil, err
	}
	switch cfg.Storage {
	case storageDB:
		if clientCfg.Domain == "" {
			return nil, &Error{Code: 400, Msg: "数据库存储器的 domain 不能为空"}
		}
		return &dbClient{id: cfg.ID, domain: clientCfg.Domain, blobs: s.Blobs}, nil
	case storageLocal:
		if clientCfg.BasePath == "" {
			return nil, &Error{Code: 400, Msg: "本地存储器的 basePath 不能为空"}
		}
		return &localClient{id: cfg.ID, basePath: clientCfg.BasePath, domain: clientCfg.Domain}, nil
	case storageFTP:
		return newFTP(cfg.ID, clientCfg)
	case storageSFTP:
		return newSFTP(cfg.ID, clientCfg)
	case storageS3:
		return newS3(clientCfg, s.HTTP)
	default:
		return nil, &Error{Code: 400, Msg: "不支持的存储器类型"}
	}
}

func validateStorage(item Config) error {
	switch item.Storage {
	case storageDB, storageLocal, storageFTP, storageSFTP, storageS3:
	default:
		return &Error{Code: 400, Msg: "不支持的存储器类型"}
	}
	if item.Storage == storageS3 {
		cfg, err := parseClient(item.Config)
		if err != nil {
			return &Error{Code: 400, Msg: "请求参数不正确"}
		}
		if _, err := normalizeS3(cfg); err != nil {
			return err
		}
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
