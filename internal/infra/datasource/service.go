package datasource

import "context"

const (
	idMaster  = 0
	codeNotOK = 1_001_007_001
	codeMiss  = 1_001_007_000
)

// Item 是返回给管理端的数据源。密码只入库，不出现在响应里。
type Item struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Username   string `json:"username"`
	CreateTime *int64 `json:"createTime"`
}

// SaveInput 是创建和修改的入参。
type SaveInput struct {
	ID       int64
	Name     string
	URL      string
	Username string
	Password string
}

// Row 是表中的一行，密码保持密文。
type Row struct {
	ID         int64
	Name       string
	URL        string
	Username   string
	Password   string
	CreateTime int64
}

// Store 读写 infra_data_source_config。这张表忽略租户。
type Store interface {
	Insert(ctx context.Context, row Row) (int64, error)
	Update(ctx context.Context, row Row) error
	Get(ctx context.Context, id int64) (*Row, error)
	List(ctx context.Context) ([]Row, error)
	Delete(ctx context.Context, id int64) error
	DeleteList(ctx context.Context, ids []int64) error
}

// Error 是数据源配置的业务错误。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Service 在写入前确认连接，并用显式注入的 Java 兼容密钥加密密码。
type Service struct {
	Store  Store
	Key    string
	Master Item
	Ping   func(ctx context.Context, url, username, password string) error
}

func (s *Service) ping(ctx context.Context, url, username, password string) error {
	if s.Ping != nil {
		return s.Ping(ctx, url, username, password)
	}
	return PingMySQL(ctx, url, username, password)
}

func (s *Service) Create(ctx context.Context, in SaveInput) (int64, error) {
	// 密钥缺失时先拒绝请求，不去连接用户填写的数据库地址。
	secret, err := EncryptBase64(s.Key, in.Password)
	if err != nil {
		return 0, &Error{Code: 500, Msg: "系统异常"}
	}
	if err := s.ping(ctx, in.URL, in.Username, in.Password); err != nil {
		return 0, err
	}
	return s.Store.Insert(ctx, Row{Name: in.Name, URL: in.URL, Username: in.Username, Password: secret})
}

func (s *Service) Update(ctx context.Context, in SaveInput) error {
	current, err := s.Store.Get(ctx, in.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: codeMiss, Msg: "数据源配置不存在"}
	}
	secret, err := EncryptBase64(s.Key, in.Password)
	if err != nil {
		return &Error{Code: 500, Msg: "系统异常"}
	}
	if err := s.ping(ctx, in.URL, in.Username, in.Password); err != nil {
		return err
	}
	return s.Store.Update(ctx, Row{ID: in.ID, Name: in.Name, URL: in.URL, Username: in.Username, Password: secret})
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	current, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return &Error{Code: codeMiss, Msg: "数据源配置不存在"}
	}
	return s.Store.Delete(ctx, id)
}

func (s *Service) DeleteList(ctx context.Context, ids []int64) error {
	return s.Store.DeleteList(ctx, ids)
}

func (s *Service) Get(ctx context.Context, id int64) (*Item, error) {
	if id == idMaster {
		item := s.Master
		item.ID = idMaster
		if item.Name == "" {
			item.Name = "master"
		}
		return &item, nil
	}
	row, err := s.Store.Get(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	item := toItem(*row)
	return &item, nil
}

func (s *Service) List(ctx context.Context) ([]Item, error) {
	rows, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	master := s.Master
	master.ID = idMaster
	if master.Name == "" {
		master.Name = "master"
	}
	list := make([]Item, 0, len(rows)+1)
	list = append(list, master)
	for _, row := range rows {
		list = append(list, toItem(row))
	}
	return list, nil
}

func toItem(row Row) Item {
	created := row.CreateTime
	return Item{ID: row.ID, Name: row.Name, URL: row.URL, Username: row.Username, CreateTime: &created}
}
