package message

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/sheet"
)

func (h *handler) smsExport(c *gin.Context, _ caller) {
	filter, err := smsExportFilter(c)
	if err != nil {
		writeBiz(c, err)
		return
	}
	statusLabels, err := h.db.DictText(c.Request.Context(), "common_status")
	if err != nil {
		writeBiz(c, err)
		return
	}
	typeLabels, err := h.db.DictText(c.Request.Context(), "system_sms_template_type")
	if err != nil {
		writeBiz(c, err)
		return
	}
	channelLabels, err := h.db.DictText(c.Request.Context(), "system_sms_channel_code")
	if err != nil {
		writeBiz(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "短信模板.xls", "数据", []string{
		"编号", "短信签名", "开启状态", "模板编码", "模板名称", "模板内容", "备注", "短信 API 的模板编号", "短信渠道编号", "短信渠道编码", "创建时间",
	}, func(emit func([]sheet.XLSXCell) error) error {
		return h.db.SmsExportRows(c.Request.Context(), filter, func(item SmsTemplate) error {
			return emit([]sheet.XLSXCell{
				{Value: strconv.FormatInt(item.ID, 10)},
				{Value: labelInt(typeLabels, item.Type)},
				{Value: labelInt(statusLabels, item.Status)},
				{Value: item.Code},
				{Value: item.Name},
				{Value: item.Content},
				{Value: item.Remark},
				{Value: item.APITemplateID},
				{Value: strconv.FormatInt(item.ChannelID, 10)},
				{Value: labelOf(channelLabels, item.ChannelCode)},
				{Value: excelMillisValue(item.CreateTime)},
			})
		})
	})
	if err != nil {
		writeBiz(c, err)
	}
}

func (h *handler) smsLogExport(c *gin.Context, _ caller) {
	filter, err := smsLogExportFilter(c)
	if err != nil {
		writeBiz(c, err)
		return
	}
	typeLabels, err := h.db.DictText(c.Request.Context(), "system_sms_template_type")
	if err != nil {
		writeBiz(c, err)
		return
	}
	userLabels, err := h.db.DictText(c.Request.Context(), "user_type")
	if err != nil {
		writeBiz(c, err)
		return
	}
	sendLabels, err := h.db.DictText(c.Request.Context(), "system_sms_send_status")
	if err != nil {
		writeBiz(c, err)
		return
	}
	receiveLabels, err := h.db.DictText(c.Request.Context(), "system_sms_receive_status")
	if err != nil {
		writeBiz(c, err)
		return
	}
	err = sheet.WriteXLSXStream(c, "短信日志.xls", "数据", []string{
		"编号", "短信渠道编号", "短信渠道编码", "模板编号", "模板编码", "短信类型", "短信内容", "短信参数", "短信 API 的模板编号",
		"手机号", "用户编号", "用户类型", "发送状态", "发送时间", "短信 API 发送结果的编码", "短信 API 发送失败的提示",
		"短信 API 发送返回的唯一请求 ID", "短信 API 发送返回的序号", "接收状态", "接收时间", "API 接收结果的编码", "API 接收结果的说明", "创建时间",
	}, func(emit func([]sheet.XLSXCell) error) error {
		return h.db.SmsLogExportRows(c.Request.Context(), filter, func(item SmsLogRow) error {
			return emit([]sheet.XLSXCell{
				{Value: strconv.FormatInt(item.ID, 10)},
				{Value: strconv.FormatInt(item.ChannelID, 10)},
				{Value: item.ChannelCode},
				{Value: strconv.FormatInt(item.TemplateID, 10)},
				{Value: item.TemplateCode},
				{Value: labelInt(typeLabels, item.TemplateType)},
				{Value: item.TemplateContent},
				{Value: item.TemplateParams},
				{Value: item.APITemplateID},
				{Value: item.Mobile},
				{Value: strconv.FormatInt(item.UserID, 10)},
				{Value: labelInt(userLabels, item.UserType)},
				{Value: labelInt(sendLabels, item.SendStatus)},
				{Value: excelMillis(item.SendTime)},
				{Value: item.APISendCode},
				{Value: item.APISendMsg},
				{Value: item.APIRequestID},
				{Value: item.APISerialNo},
				{Value: labelInt(receiveLabels, item.ReceiveStatus)},
				{Value: excelMillis(item.ReceiveTime)},
				{Value: item.APIReceiveCode},
				{Value: item.APIReceiveMsg},
				{Value: excelMillisValue(item.CreateTime)},
			})
		})
	})
	if err != nil {
		writeBiz(c, err)
	}
}

func smsExportFilter(c *gin.Context) (SmsExportFilter, error) {
	filter := SmsExportFilter{Code: c.Query("code"), Content: c.Query("content"), APITemplateID: c.Query("apiTemplateId")}
	if text, ok := c.GetQuery("type"); ok {
		value, err := strconv.Atoi(text)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.Type = &value
	}
	if text, ok := c.GetQuery("status"); ok {
		value, err := strconv.Atoi(text)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.Status = &value
	}
	if text, ok := c.GetQuery("channelId"); ok {
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.ChannelID = &value
	}
	from, to, err := queryRange(c, "createTime")
	if err != nil {
		return filter, err
	}
	filter.CreatedFrom, filter.CreatedTo = from, to
	return filter, nil
}

func smsLogExportFilter(c *gin.Context) (SmsLogExportFilter, error) {
	filter := SmsLogExportFilter{Mobile: c.Query("mobile")}
	if text, ok := c.GetQuery("channelId"); ok {
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.ChannelID = &value
	}
	if text, ok := c.GetQuery("templateId"); ok {
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.TemplateID = &value
	}
	if text, ok := c.GetQuery("sendStatus"); ok {
		value, err := strconv.Atoi(text)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.SendStatus = &value
	}
	if text, ok := c.GetQuery("receiveStatus"); ok {
		value, err := strconv.Atoi(text)
		if err != nil {
			return filter, &Error{Code: 400, Msg: "请求参数不正确"}
		}
		filter.ReceiveStatus = &value
	}
	from, to, err := queryRange(c, "sendTime")
	if err != nil {
		return filter, err
	}
	filter.SendFrom, filter.SendTo = from, to
	from, to, err = queryRange(c, "receiveTime")
	if err != nil {
		return filter, err
	}
	filter.ReceiveFrom, filter.ReceiveTo = from, to
	return filter, nil
}

func queryRange(c *gin.Context, name string) (*time.Time, *time.Time, error) {
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
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	parse := func(text string) (*time.Time, error) {
		if text == "" {
			return nil, nil
		}
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", text, loc)
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
