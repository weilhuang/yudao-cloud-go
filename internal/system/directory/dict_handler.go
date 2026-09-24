package directory

import (
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
)

func (h *handler) dictTypeGet(c *gin.Context, _ caller) {
	id, ok := rpcID(c)
	if !ok {
		return
	}
	item, err := h.svc.Dicts.DictTypeByID(c.Request.Context(), id)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) dictDataGet(c *gin.Context, _ caller) {
	id, ok := rpcID(c)
	if !ok {
		return
	}
	item, err := h.svc.Dicts.DictDataByID(c.Request.Context(), id)
	if err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) dictTypeDeleteList(c *gin.Context, _ caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if len(ids) == 0 {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "字典类型编号不能为空"})
		return
	}
	if err := h.svc.DeleteDictTypeList(c.Request.Context(), ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) dictDataDeleteList(c *gin.Context, _ caller) {
	ids, ok := rpcIDs(c, "ids")
	if !ok {
		return
	}
	if len(ids) == 0 {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "字典数据编号不能为空"})
		return
	}
	if err := h.svc.DeleteDictDataList(c.Request.Context(), ids); err != nil {
		writeErr(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) dictTypeExport(c *gin.Context, _ caller) {
	query, ok := dictTypeQuery(c)
	if !ok {
		return
	}
	query.PageSize = -1
	labels, err := h.svc.Reader.PostStatusLabels(c.Request.Context())
	if err != nil {
		writeErr(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "字典类型.xls", "数据", []string{"字典主键", "字典名称", "字典类型", "状态"},
		func(emit func([]sheet.XLSXCell) error) error {
			return h.svc.Dicts.DictTypeExportRows(c.Request.Context(), query, func(item DictType) error {
				// Java 的 Long 主键作为文本写入，避免 Excel 超过 15 位时丢精度。
				return emit([]sheet.XLSXCell{
					{Value: strconv.FormatInt(item.ID, 10)},
					{Value: item.Name},
					{Value: item.Type},
					{Value: labels[item.Status]},
				})
			})
		})
	if err != nil {
		writeErr(c, err)
	}
}

func (h *handler) dictDataExport(c *gin.Context, _ caller) {
	_, _, status, ok := dictPageQuery(c)
	if !ok || !dictDataFieldsValid(c) {
		return
	}
	labels, err := h.svc.Reader.PostStatusLabels(c.Request.Context())
	if err != nil {
		writeErr(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "字典数据.xls", "数据",
		[]string{"字典编码", "字典排序", "字典标签", "字典键值", "字典类型", "状态"},
		func(emit func([]sheet.XLSXCell) error) error {
			return h.svc.Dicts.DictDataExportRows(c.Request.Context(), c.Query("label"), c.Query("dictType"), status,
				func(item DictData) error {
					return emit([]sheet.XLSXCell{
						{Value: strconv.FormatInt(item.ID, 10)},
						{Value: strconv.Itoa(item.Sort), Numeric: true},
						{Value: item.Label},
						{Value: item.Value},
						{Value: item.DictType},
						{Value: labels[item.Status]},
					})
				})
		})
	if err != nil {
		writeErr(c, err)
	}
}

// dictPageQuery 在导出强制不分页前，先按 Java PageParam 校验请求参数。
func dictPageQuery(c *gin.Context) (int, int, *int, bool) {
	pageNo, pageSize := 1, 10
	if raw, present := c.GetQuery("pageNo"); present {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "页码最小值为 1"})
			return 0, 0, nil, false
		}
		pageNo = value
	}
	if raw, present := c.GetQuery("pageSize"); present {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "每页条数必须在 1 到 200 之间"})
			return 0, 0, nil, false
		}
		pageSize = value
	}
	var status *int
	if raw, present := c.GetQuery("status"); present && raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "字典状态不正确"})
			return 0, 0, nil, false
		}
		status = &value
	}
	return pageNo, pageSize, status, true
}

func dictDataFieldsValid(c *gin.Context) bool {
	if utf8.RuneCountInString(c.Query("label")) > 100 || utf8.RuneCountInString(c.Query("dictType")) > 100 {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "字典标签或类型长度不能超过 100 个字符"})
		return false
	}
	if raw := c.Query("status"); raw != "" && raw != "0" && raw != "1" {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "字典状态不正确"})
		return false
	}
	return true
}

func dictTypeQuery(c *gin.Context) (DictTypeQuery, bool) {
	pageNo, pageSize, status, ok := dictPageQuery(c)
	if !ok {
		return DictTypeQuery{}, false
	}
	if utf8.RuneCountInString(c.Query("type")) > 100 {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "字典类型长度不能超过 100 个字符"})
		return DictTypeQuery{}, false
	}
	start, end, ok := dictCreateTimeRange(c)
	if !ok {
		return DictTypeQuery{}, false
	}
	return DictTypeQuery{
		PageNo: pageNo, PageSize: pageSize, Name: c.Query("name"), Type: c.Query("type"),
		Status: status, CreateStart: start, CreateEnd: end,
	}, true
}

// Spring 可接收 createTime=起&createTime=止；Vben 的数组序列化也可能用索引键。
func dictCreateTimeRange(c *gin.Context) (*time.Time, *time.Time, bool) {
	values := c.QueryArray("createTime")
	if len(values) == 0 {
		values = c.QueryArray("createTime[]")
	}
	if len(values) == 0 {
		values = []string{c.Query("createTime[0]"), c.Query("createTime[1]")}
	}
	var bounds [2]*time.Time
	for i := 0; i < len(values) && i < 2; i++ {
		if values[i] == "" {
			continue
		}
		value, err := time.ParseInLocation(time.DateTime, values[i], time.Local)
		if err != nil {
			writeErr(c, &Error{Code: codeBadRequest, Msg: "创建时间格式不正确"})
			return nil, nil, false
		}
		bounds[i] = &value
	}
	if bounds[0] != nil && bounds[1] != nil && bounds[0].After(*bounds[1]) {
		writeErr(c, &Error{Code: codeBadRequest, Msg: "创建时间范围不正确"})
		return nil, nil, false
	}
	return bounds[0], bounds[1], true
}
