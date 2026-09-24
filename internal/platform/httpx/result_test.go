package httpx

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResultLongSerializationBoundary(t *testing.T) {
	cases := []struct {
		name string
		id   int64
		want string
	}{
		{"positive below", 9007199254740990, `9007199254740990`},
		{"positive boundary", 9007199254740991, `"9007199254740991"`},
		{"positive above", 9007199254740992, `"9007199254740992"`},
		{"negative below", -9007199254740990, `-9007199254740990`},
		{"negative boundary", -9007199254740991, `"-9007199254740991"`},
		{"negative above", -9007199254740992, `"-9007199254740992"`},
		{"max int64", math.MaxInt64, `"9223372036854775807"`},
		{"min int64", math.MinInt64, `"-9223372036854775808"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(Result{Data: struct {
				ID int64 `json:"id"`
			}{ID: tc.id}})
			if err != nil {
				t.Fatal(err)
			}
			want := `{"code":0,"msg":"","data":{"id":` + tc.want + `}}`
			if string(got) != want {
				t.Fatalf("JSON = %s, want %s", got, want)
			}
		})
	}
}

func TestResultOnlyRewritesIntegerTokens(t *testing.T) {
	data := map[string]any{
		"text":    `ID 9007199254740992, quote: " and slash: \\`,
		"decimal": json.Number("9007199254740992.5"),
		"number":  json.Number("9007199254740992"),
		"float":   float64(9007199254740992),
		"nested":  []any{int64(9007199254740992), map[string]any{"id": int64(-9007199254740992)}},
	}
	got, err := json.Marshal(Result{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("invalid JSON: %s", got)
	}
	var parsed struct {
		Data struct {
			Text    string          `json:"text"`
			Decimal json.Number     `json:"decimal"`
			Number  json.Number     `json:"number"`
			Float   json.Number     `json:"float"`
			Nested  json.RawMessage `json:"nested"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data.Text != data["text"] || parsed.Data.Decimal != "9007199254740992.5" ||
		parsed.Data.Number != "9007199254740992" || parsed.Data.Float != "9007199254740992" {
		t.Fatalf("scalar values changed: %+v", parsed.Data)
	}
	if string(parsed.Data.Nested) != `["9007199254740992",{"id":"-9007199254740992"}]` {
		t.Fatalf("nested IDs = %s", parsed.Data.Nested)
	}
}

func TestResultPreservesStandardJSONOptions(t *testing.T) {
	data := struct {
		ID       int64   `json:"id,string"`
		Omitted  *int64  `json:"omitted,omitempty"`
		Bytes    []byte  `json:"bytes"`
		Float    float64 `json:"float"`
		Nullable []int64 `json:"nullable"`
	}{
		ID:    9007199254740992,
		Bytes: []byte{0, 1, 2},
		Float: float64(9007199254740992),
	}
	got, err := json.Marshal(Result{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"code":0,"msg":"","data":{"id":"9007199254740992","bytes":"AAEC","float":9007199254740992,"nullable":null}}`
	if string(got) != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

type longEmbedded struct {
	ID int64 `json:"id"`
}

type customPointerChild struct {
	ID int64 `json:"id"`
}

func (*customPointerChild) MarshalJSON() ([]byte, error) {
	return []byte(`"custom"`), nil
}

func TestResultEmbeddedLongAndPointerMarshaler(t *testing.T) {
	// 匿名嵌入字段会被 encoding/json 提升到外层。
	embedded, err := json.Marshal(Result{Data: struct {
		longEmbedded
		Name string `json:"name"`
	}{longEmbedded: longEmbedded{ID: 9007199254740992}, Name: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	// 上面的匿名字段类型是未导出的，标准库仍会展开其中导出的 ID。
	if string(embedded) != `{"code":0,"msg":"","data":{"id":"9007199254740992","name":"test"}}` {
		t.Fatalf("embedded JSON = %s", embedded)
	}
	parent := &struct {
		Child customPointerChild `json:"child"`
	}{Child: customPointerChild{ID: 9007199254740992}}
	custom, err := json.Marshal(Result{Data: parent})
	if err != nil {
		t.Fatal(err)
	}
	if string(custom) != `{"code":0,"msg":"","data":{"child":"custom"}}` {
		t.Fatalf("pointer marshaler JSON = %s", custom)
	}
}

func TestResultIntegerMapKeyAndRawMessage(t *testing.T) {
	data := struct {
		ByID map[int64]any   `json:"byId"`
		Raw  json.RawMessage `json:"raw"`
	}{
		ByID: map[int64]any{1: int64(9007199254740992)},
		Raw:  json.RawMessage(`{"id":9007199254740992}`),
	}
	got, err := json.Marshal(Result{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"code":0,"msg":"","data":{"byId":{"1":"9007199254740992"},"raw":{"id":9007199254740992}}}`
	if string(got) != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

type textMapKey int

func (textMapKey) MarshalText() ([]byte, error) { return []byte("mapped"), nil }

func TestResultTextMapKeyAndEscapedKey(t *testing.T) {
	data := map[string]any{
		"text-key": map[textMapKey]any{1: int64(9007199254740992)},
		"a<\"":     int64(9007199254740992),
	}
	got, err := json.Marshal(Result{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.Data["a<\""]) != `"9007199254740992"` ||
		string(decoded.Data["text-key"]) != `{"mapped":"9007199254740992"}` {
		t.Fatalf("escaped/text-key JSON = %s", got)
	}
}

type customJSON struct{}

func (customJSON) MarshalJSON() ([]byte, error) {
	return []byte(`{"id":9007199254740992}`), nil
}

func TestResultKeepsCustomMarshalerOutput(t *testing.T) {
	got, err := json.Marshal(Result{Data: customJSON{}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"code":0,"msg":"","data":{"id":9007199254740992}}`
	if string(got) != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

type longBenchmarkRow struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     int    `json:"status"`
	TenantID   int64  `json:"tenantId"`
	Kind       int    `json:"kind"`
	CreateTime int64  `json:"createTime"`
}

func longBenchmarkData() any {
	rows := make([]longBenchmarkRow, 100)
	for i := range rows {
		rows[i] = longBenchmarkRow{ID: 9007199254740992 + int64(i), Name: "测试用户", Status: 1, TenantID: 1, Kind: 3, CreateTime: 1789563600000}
	}
	return struct {
		List  []longBenchmarkRow `json:"list"`
		Total int64              `json:"total"`
	}{List: rows, Total: 100}
}

// 两个基准使用同一批 100 行数据，便于查看 Long 兼容相对标准库的成本。
func BenchmarkResultLongMarshal(b *testing.B) {
	result := Result{Data: longBenchmarkData()}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(result); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResultLongDirect(b *testing.B) {
	result := Result{Data: longBenchmarkData()}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := result.MarshalJSON(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlainResultMarshal(b *testing.B) {
	result := struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data any    `json:"data"`
	}{Data: longBenchmarkData()}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(result); err != nil {
			b.Fatal(err)
		}
	}
}

func TestOKUsesLongSerializationForHTTPResponse(t *testing.T) {
	r := gin.New()
	r.GET("/id", func(c *gin.Context) { OK(c, map[string]any{"id": int64(math.MaxInt64)}) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/id", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		!strings.Contains(rec.Body.String(), `"id":"9223372036854775807"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestResultSerializationErrorBeforeWrite(t *testing.T) {
	r := gin.New()
	r.GET("/invalid", func(c *gin.Context) { OK(c, make(chan int)) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invalid", nil))
	if rec.Code != http.StatusInternalServerError ||
		rec.Body.String() != `{"code":500,"msg":"系统异常","data":null}` {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
