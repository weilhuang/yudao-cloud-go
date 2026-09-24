package directory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type dictRPCFake struct {
	list       []RPCDictData
	listCalls  int
	valueCalls int
	seenType   string
	seenValues []string
}

func (*dictRPCFake) RPCUsers(context.Context, int64, RPCUserFilter) ([]RPCUser, error) {
	return nil, nil
}
func (*dictRPCFake) RPCDepts(context.Context, int64, []int64) ([]RPCDept, error) {
	return nil, nil
}
func (*dictRPCFake) RPCChildDepts(context.Context, int64, []int64) ([]RPCDept, error) {
	return nil, nil
}
func (f *dictRPCFake) RPCDictDataList(_ context.Context, typ string) ([]RPCDictData, error) {
	f.listCalls++
	f.seenType = typ
	return f.list, nil
}
func (f *dictRPCFake) RPCDictDataValues(_ context.Context, typ string, values []string) ([]RPCDictData, error) {
	f.valueCalls++
	f.seenType = typ
	f.seenValues = append([]string(nil), values...)
	return f.list, nil
}

type dictRPCResult struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func TestDictRPCContract(t *testing.T) {
	reader := &dictRPCFake{list: []RPCDictData{
		{Label: "男", Value: "1", DictType: "sys_sex", Status: 0},
		{Label: "女", Value: "2", DictType: "sys_sex", Status: 1},
	}}
	r := gin.New()
	MountRPC(r, reader, func(_ context.Context, tenantID int64, _ time.Time) error {
		if tenantID != 1 && tenantID != 2 {
			return &Error{Code: 1_002_015_001, Msg: "租户不存在"}
		}
		return nil
	}, nil)
	call := func(path, tenant string) dictRPCResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if tenant != "" {
			req.Header.Set("tenant-id", tenant)
		}
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s HTTP %d: %s", path, w.Code, w.Body.String())
		}
		var res dictRPCResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	// list 仅有 Java DTO 的四个字段，禁用项仍在列表中。
	res := call("/rpc-api/system/dict-data/list?dictType=sys_sex", "1")
	if res.Code != 0 || reader.seenType != "sys_sex" || reader.listCalls != 1 {
		t.Fatalf("列表响应错误：%+v", res)
	}
	var raw []map[string]any
	if err := json.Unmarshal(res.Data, &raw); err != nil || len(raw) != 2 {
		t.Fatalf("列表解析失败：%s %v", res.Data, err)
	}
	for _, item := range raw {
		if len(item) != 4 {
			t.Fatalf("DTO 多出字段：%v", item)
		}
	}
	if raw[1]["status"] != float64(1) {
		t.Fatalf("禁用字典被过滤：%v", raw)
	}
	// 字典查询为全局数据；另一个合法租户收到同一列表。
	if res := call("/rpc-api/system/dict-data/list?dictType=sys_sex", "2"); res.Code != 0 || string(res.Data) != string(mustJSON(t, reader.list)) {
		t.Fatalf("全局字典按租户分区：%+v", res)
	}

	checks := []struct {
		path   string
		tenant string
		code   int
		msg    string
	}{
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=1", "1", 0, ""},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=1,1", "1", 0, ""},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=1&values=1", "1", 0, ""},
		{"/rpc-api/system/dict-data/valid?dictType=unknown&values=", "1", 0, ""},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=3", "1", 1_002_007_001, "当前字典数据不存在"},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=2", "1", 1_002_007_002, "字典数据(女)不处于开启状态，不允许选择"},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=3,2", "1", 1_002_007_001, "当前字典数据不存在"},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex&values=1&values=2,3", "1", 1_002_007_001, "当前字典数据不存在"},
		{"/rpc-api/system/dict-data/valid?dictType=sys_sex", "1", 400, "请求参数不正确"},
		{"/rpc-api/system/dict-data/valid?values=1", "1", 400, "请求参数不正确"},
		{"/rpc-api/system/dict-data/list", "1", 400, "请求参数不正确"},
		{"/rpc-api/system/dict-data/list?dictType=sys_sex", "", 400, "请求的租户标识未传递，请进行排查"},
		{"/rpc-api/system/dict-data/list?dictType=sys_sex", "3", 1_002_015_001, "租户不存在"},
	}
	for _, tc := range checks {
		res := call(tc.path, tc.tenant)
		if res.Code != tc.code || res.Msg != tc.msg {
			t.Fatalf("%s tenant=%s: got=%+v, want code=%d msg=%s", tc.path, tc.tenant, res, tc.code, tc.msg)
		}
		if res.Code == 0 && tc.path == "/rpc-api/system/dict-data/valid?dictType=unknown&values=" && string(res.Data) != "true" {
			t.Fatalf("空集应该返回 true：%s", res.Data)
		}
	}
	if reader.valueCalls != 7 { // 空集与参数错误均不应读字典表。
		t.Fatalf("值查询次数错误：%d", reader.valueCalls)
	}
	if !reflect.DeepEqual(reader.seenValues, []string{"1", "2,3"}) {
		t.Fatalf("重复 values 参数不应二次拆分：%v", reader.seenValues)
	}

	reader.list = nil
	if res := call("/rpc-api/system/dict-data/list?dictType=empty", "1"); res.Code != 0 || string(res.Data) != "[]" {
		t.Fatalf("空列表不是 []：%+v", res)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDictRPCPropagatesStoreError(t *testing.T) {
	// 保证数据库错误不会伪装成字典值缺失。
	reader := &failingDictRPCReader{err: errors.New("sql failed")}
	r := gin.New()
	MountRPC(r, reader, func(context.Context, int64, time.Time) error { return nil }, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/rpc-api/system/dict-data/valid?dictType=x&values=a", nil)
	req.Header.Set("tenant-id", "1")
	r.ServeHTTP(w, req)
	var res dictRPCResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Code != 500 {
		t.Fatalf("SQL 错误被误判：%s %v", w.Body.String(), err)
	}
}

type failingDictRPCReader struct {
	dictRPCFake
	err error
}

func (f *failingDictRPCReader) RPCDictDataValues(context.Context, string, []string) ([]RPCDictData, error) {
	return nil, f.err
}
