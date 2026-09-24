package datasource

import (
	"context"
	"testing"
)

const testEncryptorPassword = "0123456789abcdef"

func TestEncryptMatchesHutoolRoundTrip(t *testing.T) {
	const key = testEncryptorPassword
	encoded, err := EncryptBase64(key, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "D89D/OvFrItmxI9ct8JbAg==" {
		t.Fatalf("与 Hutool AES/ECB 不一致：%s", encoded)
	}
	plain, err := DecryptBase64(key, encoded)
	if err != nil || plain != "123456" {
		t.Fatalf("还原失败：%q %v", plain, err)
	}
	again, err := EncryptBase64(key, "123456")
	if err != nil || again != encoded {
		t.Fatalf("ECB 应稳定：%s %s %v", encoded, again, err)
	}
}

type memStore struct {
	rows map[int64]Row
	next int64
}

func (m *memStore) Insert(_ context.Context, row Row) (int64, error) {
	if m.rows == nil {
		m.rows = map[int64]Row{}
	}
	m.next++
	row.ID = m.next
	m.rows[row.ID] = row
	return row.ID, nil
}
func (m *memStore) Update(_ context.Context, row Row) error {
	m.rows[row.ID] = row
	return nil
}
func (m *memStore) Get(_ context.Context, id int64) (*Row, error) {
	row, ok := m.rows[id]
	if !ok {
		return nil, nil
	}
	return &row, nil
}
func (m *memStore) List(context.Context) ([]Row, error) {
	list := make([]Row, 0, len(m.rows))
	for _, row := range m.rows {
		list = append(list, row)
	}
	return list, nil
}
func (m *memStore) Delete(_ context.Context, id int64) error {
	delete(m.rows, id)
	return nil
}
func (m *memStore) DeleteList(_ context.Context, ids []int64) error {
	for _, id := range ids {
		delete(m.rows, id)
	}
	return nil
}

func TestCreateRejectsBadConnectionAndHidesPassword(t *testing.T) {
	store := &memStore{}
	svc := &Service{
		Store:  store,
		Key:    testEncryptorPassword,
		Master: Item{ID: 0, Name: "master", URL: "jdbc:mysql://127.0.0.1:3306/ruoyi-vue-pro", Username: "root"},
		Ping: func(context.Context, string, string, string) error {
			return &Error{Code: codeNotOK, Msg: "数据源配置不正确，无法进行连接"}
		},
	}
	if _, err := svc.Create(context.Background(), SaveInput{Name: "从库", URL: "jdbc:mysql://127.0.0.1:1/none", Username: "root", Password: "123456"}); err == nil || err.(*Error).Code != codeNotOK || len(store.rows) != 0 {
		t.Fatalf("连接失败不应落库：%v %+v", err, store.rows)
	}
	pinged := false
	svc.Ping = func(context.Context, string, string, string) error { pinged = true; return nil }
	svc.Key = ""
	if _, err := svc.Create(context.Background(), SaveInput{Name: "从库", URL: "jdbc:mysql://127.0.0.1:3306/demo", Username: "root", Password: "123456"}); err == nil || len(store.rows) != 0 || pinged {
		t.Fatal("缺少显式密钥时不得连接或落库")
	}
	svc.Key = testEncryptorPassword
	id, err := svc.Create(context.Background(), SaveInput{Name: "从库", URL: "jdbc:mysql://127.0.0.1:3306/demo", Username: "root", Password: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptBase64(testEncryptorPassword, store.rows[id].Password)
	if err != nil || plain != "123456" {
		t.Fatalf("库存密码应能被 Java 解开：%q %v", store.rows[id].Password, err)
	}
	list, err := svc.List(context.Background())
	if err != nil || len(list) != 2 || list[0].ID != 0 || list[1].ID != id {
		t.Fatalf("列表应先放主库：%+v %v", list, err)
	}
	if err := svc.Delete(context.Background(), 0); err == nil || err.(*Error).Msg != "数据源配置不存在" {
		t.Fatalf("主库不在表里，单删应失败：%v", err)
	}
	master, err := svc.Get(context.Background(), 0)
	if err != nil || master == nil || master.Name != "master" {
		t.Fatalf("编号 0 应返回主库：%+v %v", master, err)
	}
}
