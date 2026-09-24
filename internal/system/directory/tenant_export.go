package directory

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
)

// TenantExportFilter 对齐租户分页的筛选，导出不受每页条数限制。
type TenantExportFilter struct {
	Name          string
	ContactName   string
	ContactMobile string
	Status        *int
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

func (m *MySQL) TenantExportRows(ctx context.Context, filter TenantExportFilter, emit func(Tenant) error) error {
	where := `WHERE deleted=0`
	var args []any
	if filter.Name != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+filter.Name+"%")
	}
	if filter.ContactName != "" {
		where += ` AND contact_name LIKE ?`
		args = append(args, "%"+filter.ContactName+"%")
	}
	if filter.ContactMobile != "" {
		where += ` AND contact_mobile LIKE ?`
		args = append(args, "%"+filter.ContactMobile+"%")
	}
	if filter.Status != nil {
		where += ` AND status=?`
		args = append(args, *filter.Status)
	}
	if filter.CreatedFrom != nil {
		where += ` AND create_time >= ?`
		args = append(args, *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		where += ` AND create_time <= ?`
		args = append(args, *filter.CreatedTo)
	}
	rows, err := m.DB.QueryContext(ctx, tenantSelect+` `+where+` ORDER BY id DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanTenant(rows)
		if err != nil {
			return err
		}
		if err := emit(*item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (h *handler) tenantExport(c *gin.Context, _ caller) {
	filter := TenantExportFilter{Name: c.Query("name"), ContactName: c.Query("contactName"), ContactMobile: c.Query("contactMobile")}
	if text, ok := c.GetQuery("status"); ok {
		value, err := strconv.Atoi(text)
		if err != nil {
			writeErr(c, &Error{Code: 400, Msg: "请求参数不正确"})
			return
		}
		filter.Status = &value
	}
	from, to, err := tenantTimeRange(c, "createTime")
	if err != nil {
		writeErr(c, err)
		return
	}
	filter.CreatedFrom, filter.CreatedTo = from, to
	labels, err := h.svc.DictLabels(c.Request.Context(), "common_status")
	if err != nil {
		writeErr(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "租户.xls", "数据", []string{"租户编号", "租户名", "联系人", "联系手机", "状态", "创建时间"}, func(emit func([]sheet.XLSXCell) error) error {
		return h.svc.Tenants.TenantExportRows(c.Request.Context(), filter, func(item Tenant) error {
			created := item.CreateTime
			return emit([]sheet.XLSXCell{
				{Value: strconv.FormatInt(item.ID, 10)},
				{Value: item.Name},
				{Value: item.ContactName},
				{Value: item.ContactMobile},
				{Value: dictLabelInt(labels, item.Status)},
				{Value: excelTime(&created)},
			})
		})
	})
	if err != nil {
		writeErr(c, err)
	}
}

func tenantTimeRange(c *gin.Context, name string) (*time.Time, *time.Time, error) {
	values := c.QueryArray(name)
	if len(values) == 0 {
		if value, ok := c.GetQuery(name + "[0]"); ok {
			values = append(values, value)
		}
		if value, ok := c.GetQuery(name + "[1]"); ok {
			values = append(values, value)
		}
	}
	if len(values) > 2 {
		return nil, nil, &Error{Code: 400, Msg: "请求参数不正确"}
	}
	parse := func(text string) (*time.Time, error) {
		if text == "" {
			return nil, nil
		}
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", text, shanghai)
		if err != nil {
			return nil, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		return &parsed, nil
	}
	var from, to *time.Time
	var err error
	if len(values) >= 1 {
		from, err = parse(values[0])
		if err != nil {
			return nil, nil, err
		}
	}
	if len(values) >= 2 {
		to, err = parse(values[1])
		if err != nil {
			return nil, nil, err
		}
	}
	return from, to, nil
}
