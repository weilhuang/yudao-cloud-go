// Package httpx 提供和芋道 CommonResult 对齐的 HTTP 外壳。
// 字段顺序按 Java 声明：code、msg、data。前端不依赖顺序，但对照夹具时少一次差异。
package httpx

import (
	"bytes"
	"encoding"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// 与 Java GlobalErrorCodeConstants 对齐的码。
const (
	CodeOK       = 0
	CodeInternal = 500
)

// Result 是管理后台已经在用的响应包体。
type Result struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

const maxSafeInteger int64 = 9007199254740991

// MarshalJSON 对齐 mini 的 Long 边界：绝对值达到 2^53-1 时写成字符串。
// 先让标准库完整编码一次，再按原始 Go 类型和 JSON 路径修整数字；这样保留
// omitempty、匿名嵌入、指针接收者 MarshalJSON、[]byte 和字段顺序。
func (r Result) MarshalJSON() ([]byte, error) {
	type plainResult Result // 消除本方法，避免递归调用。
	plain := plainResult(r)
	raw, err := json.Marshal(plain)
	if err != nil {
		return nil, err
	}
	rewriter := longJSONRewriter{raw: raw, fields: make(map[reflect.Type]map[string][]int)}
	rewriter.value(0, reflect.ValueOf(plain))
	if rewriter.out == nil {
		return raw, nil
	}
	return append(rewriter.out, raw[rewriter.copied:]...), nil
}

// longJSONRewriter 只改写已经由 encoding/json 产生的整数 token。
// 原始反射值负责区分 int64、float64 和自定义 Marshaler，不重新组装 JSON。
type longJSONRewriter struct {
	raw    []byte
	out    []byte
	copied int
	fields map[reflect.Type]map[string][]int
}

func (w *longJSONRewriter) value(pos int, value reflect.Value) int {
	pos = w.spaces(pos)
	if pos >= len(w.raw) {
		return pos
	}
	value = jsonValue(value)
	switch w.raw[pos] {
	case '{':
		return w.object(pos, value)
	case '[':
		return w.array(pos, value)
	case '"':
		return w.stringEnd(pos)
	default:
		end := pos
		for end < len(w.raw) && w.raw[end] != ',' && w.raw[end] != '}' && w.raw[end] != ']' &&
			w.raw[end] != ' ' && w.raw[end] != '\n' && w.raw[end] != '\r' && w.raw[end] != '\t' {
			end++
		}
		if pos < end && (w.raw[pos] == '-' || w.raw[pos] >= '0' && w.raw[pos] <= '9') &&
			longToken(value, w.raw[pos:end]) {
			w.quote(pos, end)
		}
		return end
	}
}

func (w *longJSONRewriter) object(pos int, value reflect.Value) int {
	pos++
	for {
		pos = w.spaces(pos)
		if pos >= len(w.raw) || w.raw[pos] == '}' {
			return pos + 1
		}
		keyStart := pos + 1
		end := w.stringEnd(pos)
		keyBytes := w.raw[keyStart : end-1]
		if bytes.IndexByte(keyBytes, '\\') >= 0 {
			// encoding/json 会转义引号、反斜杠和部分 Unicode 键名。
			decoded, _ := strconv.Unquote(string(w.raw[keyStart-1 : end]))
			keyBytes = []byte(decoded)
		}
		pos = w.spaces(end) + 1 // 跳过冒号。raw 已由标准库验证为合法 JSON。
		var child reflect.Value
		if w.needsOriginalValue(pos) {
			child = w.objectValue(value, keyBytes)
		}
		pos = w.value(pos, child)
		pos = w.spaces(pos)
		if pos < len(w.raw) && w.raw[pos] == ',' {
			pos++
		}
	}
}

func (w *longJSONRewriter) array(pos int, value reflect.Value) int {
	pos++
	index := 0
	for {
		pos = w.spaces(pos)
		if pos >= len(w.raw) || w.raw[pos] == ']' {
			return pos + 1
		}
		var child reflect.Value
		if w.needsOriginalValue(pos) && value.IsValid() &&
			(value.Kind() == reflect.Slice || value.Kind() == reflect.Array) && index < value.Len() {
			child = value.Index(index)
		}
		pos = w.value(pos, child)
		index++
		pos = w.spaces(pos)
		if pos < len(w.raw) && w.raw[pos] == ',' {
			pos++
		}
	}
}

// 小整数、字符串、布尔值和 null 不可能需要 Long 修整；这些常见字段直接
// 跳过反射查找。对象和数组仍需追踪其内部的 Long 路径。
func (w *longJSONRewriter) needsOriginalValue(pos int) bool {
	pos = w.spaces(pos)
	if pos >= len(w.raw) {
		return false
	}
	if w.raw[pos] == '{' || w.raw[pos] == '[' {
		return true
	}
	if w.raw[pos] != '-' && (w.raw[pos] < '0' || w.raw[pos] > '9') {
		return false
	}
	start := pos
	if w.raw[pos] == '-' {
		pos++
	}
	for pos < len(w.raw) && w.raw[pos] >= '0' && w.raw[pos] <= '9' {
		pos++
	}
	if pos < len(w.raw) && (w.raw[pos] == '.' || w.raw[pos] == 'e' || w.raw[pos] == 'E') {
		return false
	}
	digits := w.raw[start:pos]
	if digits[0] == '-' {
		digits = digits[1:]
	}
	limit := []byte("9007199254740991")
	return len(digits) > len(limit) || len(digits) == len(limit) && bytes.Compare(digits, limit) >= 0
}

var textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

func (w *longJSONRewriter) objectValue(value reflect.Value, keyBytes []byte) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Struct:
		// string([]byte) 仅用于 map 查询，不生成持久副本。
		index, ok := w.structFields(value.Type())[string(keyBytes)]
		if !ok {
			return reflect.Value{}
		}
		for _, part := range index {
			for value.Kind() == reflect.Pointer {
				if value.IsNil() {
					return reflect.Value{}
				}
				value = value.Elem()
			}
			value = value.Field(part)
		}
		return value
	case reflect.Map:
		return w.mapValue(value, string(keyBytes))
	}
	return reflect.Value{}
}

func (w *longJSONRewriter) mapValue(value reflect.Value, key string) reflect.Value {
	if value.IsNil() {
		return reflect.Value{}
	}
	keyType := value.Type().Key()
	if keyType.Kind() == reflect.String {
		return value.MapIndex(reflect.ValueOf(key).Convert(keyType))
	}
	if keyType.Implements(textMarshalerType) {
		// 罕见的 TextMarshaler map 键由标准库编码为字符串。
		iter := value.MapRange()
		for iter.Next() {
			if iter.Key().Kind() == reflect.Pointer && iter.Key().IsNil() {
				if key == "" {
					return iter.Value()
				}
				continue
			}
			marshaler, ok := iter.Key().Interface().(encoding.TextMarshaler)
			if !ok {
				continue
			}
			encoded, err := marshaler.MarshalText()
			if err == nil && string(encoded) == key {
				return iter.Value()
			}
		}
		return reflect.Value{}
	}
	var mapKey reflect.Value
	switch keyType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(key, 10, keyType.Bits())
		if err == nil {
			mapKey = reflect.ValueOf(parsed).Convert(keyType)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(key, 10, keyType.Bits())
		if err == nil {
			mapKey = reflect.ValueOf(parsed).Convert(keyType)
		}
	}
	if mapKey.IsValid() {
		return value.MapIndex(mapKey)
	}
	return reflect.Value{}
}

// jsonValue 在自定义序列化边界停止：这类类型负责自己的 JSON 内容。
func jsonValue(value reflect.Value) reflect.Value {
	for value.IsValid() {
		if value.Type().Implements(jsonMarshalerType) ||
			value.CanAddr() && reflect.PointerTo(value.Type()).Implements(jsonMarshalerType) {
			return reflect.Value{}
		}
		if value.Kind() != reflect.Interface && value.Kind() != reflect.Pointer {
			return value
		}
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return reflect.Value{}
}

func longToken(value reflect.Value, token []byte) bool {
	if !value.IsValid() {
		return false
	}
	var buf [32]byte
	switch value.Kind() {
	case reflect.Int64:
		n := value.Int()
		return (n <= -maxSafeInteger || n >= maxSafeInteger) &&
			bytes.Equal(token, strconv.AppendInt(buf[:0], n, 10))
	case reflect.Uint64:
		n := value.Uint()
		return n >= uint64(maxSafeInteger) &&
			bytes.Equal(token, strconv.AppendUint(buf[:0], n, 10))
	default:
		return false
	}
}

func (w *longJSONRewriter) quote(start, end int) {
	if w.out == nil {
		w.out = make([]byte, 0, len(w.raw)+16)
	}
	w.out = append(w.out, w.raw[w.copied:start]...)
	w.out = append(w.out, '"')
	w.out = append(w.out, w.raw[start:end]...)
	w.out = append(w.out, '"')
	w.copied = end
}

func (w *longJSONRewriter) stringEnd(pos int) int {
	for pos++; pos < len(w.raw); pos++ {
		if w.raw[pos] == '\\' {
			pos++
			continue
		}
		if w.raw[pos] == '"' {
			return pos + 1
		}
	}
	return pos
}

func (w *longJSONRewriter) spaces(pos int) int {
	for pos < len(w.raw) && (w.raw[pos] == ' ' || w.raw[pos] == '\n' || w.raw[pos] == '\r' || w.raw[pos] == '\t') {
		pos++
	}
	return pos
}

type jsonFieldCandidate struct {
	index  []int
	depth  int
	tagged bool
}

// 字段名只取决于 Go 类型；跨请求复用，避免每次分页响应都重新扫描结构体标签。
var jsonFieldsCache sync.Map // reflect.Type -> map[string][]int

// structFields 只构造类型到 JSON 字段的对应关系，不读值。标准库已经确定哪些
// 字段实际出现；此处仅按其匿名字段与同名字段的优先级找到原始类型。
func (w *longJSONRewriter) structFields(typ reflect.Type) map[string][]int {
	if cached, ok := w.fields[typ]; ok {
		return cached
	}
	if cached, ok := jsonFieldsCache.Load(typ); ok {
		fields := cached.(map[string][]int)
		w.fields[typ] = fields
		return fields
	}
	type pending struct {
		typ       reflect.Type
		index     []int
		ancestors map[reflect.Type]bool
	}
	queue := []pending{{typ: typ, ancestors: map[reflect.Type]bool{typ: true}}}
	candidates := make(map[string][]jsonFieldCandidate)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for i := 0; i < current.typ.NumField(); i++ {
			field := current.typ.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			index := append(append([]int(nil), current.index...), i)
			fieldType := field.Type
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if field.Anonymous && name == "" && fieldType.Kind() == reflect.Struct {
				if !current.ancestors[fieldType] {
					ancestors := make(map[reflect.Type]bool, len(current.ancestors)+1)
					for t := range current.ancestors {
						ancestors[t] = true
					}
					ancestors[fieldType] = true
					queue = append(queue, pending{typ: fieldType, index: index, ancestors: ancestors})
				}
				continue
			}
			if !field.IsExported() {
				continue
			}
			tagged := name != ""
			if name == "" {
				name = field.Name
			}
			candidates[name] = append(candidates[name], jsonFieldCandidate{index: index, depth: len(index), tagged: tagged})
		}
	}
	result := make(map[string][]int, len(candidates))
	for name, options := range candidates {
		best := options[0]
		ambiguous := false
		for _, option := range options[1:] {
			if option.depth < best.depth || option.depth == best.depth && option.tagged && !best.tagged {
				best, ambiguous = option, false
			} else if option.depth == best.depth && option.tagged == best.tagged {
				ambiguous = true
			}
		}
		if !ambiguous {
			result[name] = best.index
		}
	}
	actual, _ := jsonFieldsCache.LoadOrStore(typ, result)
	fields := actual.(map[string][]int)
	w.fields[typ] = fields
	return fields
}

// OK 写成功响应。HTTP 状态保持 200，业务是否成功看 code。
func OK(c *gin.Context, data any) {
	writeResult(c, http.StatusOK, Result{Code: CodeOK, Msg: "", Data: data})
}

// Fail 写失败响应。系统异常用 HTTP 500，msg 用固定文案，避免把内部错误直接回给浏览器。
func Fail(c *gin.Context, httpStatus, code int, msg string) {
	writeResult(c, httpStatus, Result{Code: code, Msg: msg, Data: nil})
}

// writeResult 直接写已编码的包体。若再交给 c.JSON，标准库会把 MarshalJSON
// 的结果重新扫描压缩一次；响应内容相同，但分页接口会承担额外的 CPU 和分配。
func writeResult(c *gin.Context, httpStatus int, result Result) {
	raw, err := result.MarshalJSON()
	if err != nil {
		// 序列化错误发生在响应写出前，可以安全改为固定系统错误。
		c.JSON(http.StatusInternalServerError, Result{Code: CodeInternal, Msg: "系统异常"})
		return
	}
	c.Data(httpStatus, "application/json; charset=utf-8", raw)
}
