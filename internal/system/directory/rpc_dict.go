package directory

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
)

// RPCDictData 严格投影 Java DictDataRespDTO 的四个字段。
// 列表接口不会过滤禁用状态；只有 valid 接口会拒绝禁用值。
type RPCDictData struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	DictType string `json:"dictType"`
	Status   int    `json:"status"`
}

func (h rpcHandler) dictList(c *gin.Context) {
	if _, ok := h.tenant(c); !ok {
		return
	}
	dictType, ok := c.GetQuery("dictType")
	if !ok {
		httpx.Fail(c, http.StatusOK, codeBadRequest, "请求参数不正确")
		return
	}
	list, err := h.reader.RPCDictDataList(c.Request.Context(), dictType)
	if err != nil {
		writeErr(c, err)
		return
	}
	// Java List 的空结果是 []，而 Go 的 nil slice 会编码为 null。
	if list == nil {
		list = []RPCDictData{}
	}
	httpx.OK(c, list)
}

func (h rpcHandler) dictValid(c *gin.Context) {
	if _, ok := h.tenant(c); !ok {
		return
	}
	dictType, ok := c.GetQuery("dictType")
	if !ok {
		httpx.Fail(c, http.StatusOK, codeBadRequest, "请求参数不正确")
		return
	}
	values, ok := rpcDictValues(c)
	if !ok {
		return
	}
	// Java 的 validateDictDataList 对空集合直接返回，不要求类型存在。
	if len(values) == 0 {
		httpx.OK(c, true)
		return
	}
	list, err := h.reader.RPCDictDataValues(c.Request.Context(), dictType, values)
	if err != nil {
		writeErr(c, err)
		return
	}
	byValue := make(map[string]RPCDictData, len(list))
	for _, item := range list {
		byValue[item.Value] = item
	}
	// 保留入参顺序，与 Java values.forEach 的首次错误一致。
	for _, value := range values {
		item, exists := byValue[value]
		if !exists {
			httpx.Fail(c, http.StatusOK, 1_002_007_001, "当前字典数据不存在")
			return
		}
		if item.Status != 0 {
			httpx.Fail(c, http.StatusOK, 1_002_007_002, "字典数据("+item.Label+")不处于开启状态，不允许选择")
			return
		}
	}
	httpx.OK(c, true)
}

// rpcDictValues 对齐 Spring MVC 的 Collection<String> 参数转换：单个参数按逗号
// 拆分并去空白，重复参数作为数组逐项传入。缺少 values 与显式空值不同。
func rpcDictValues(c *gin.Context) ([]string, bool) {
	raw, present := c.Request.URL.Query()["values"]
	if !present {
		httpx.Fail(c, http.StatusOK, codeBadRequest, "请求参数不正确")
		return nil, false
	}
	if len(raw) != 1 {
		return raw, true
	}
	if raw[0] == "" {
		return []string{}, true
	}
	parts := strings.Split(raw[0], ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, true
}
