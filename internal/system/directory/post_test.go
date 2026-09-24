package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

type postReaderFake struct {
	memReader
	page       Page[PostDetail]
	post       *PostDetail
	query      PostQuery
	pageCalls  int
	labels     map[int]string
	labelCalls int
}

func (f *postReaderFake) PostPage(_ context.Context, _ int64, query PostQuery) (Page[PostDetail], error) {
	f.query = query
	f.pageCalls++
	return f.page, nil
}
func (f *postReaderFake) PostGet(_ context.Context, _ int64, _ int64) (*PostDetail, error) {
	return f.post, nil
}
func (f *postReaderFake) PostExportRows(_ context.Context, _ int64, query PostQuery, emit func(PostDetail) error) error {
	f.query = query
	for _, item := range f.page.List {
		if err := emit(item); err != nil {
			return err
		}
	}
	return nil
}
func (f *postReaderFake) PostStatusLabels(context.Context) (map[int]string, error) {
	f.labelCalls++
	return f.labels, nil
}

type postAccessFake struct {
	memAccess
	ids []int64
}

func (f *postAccessFake) DeletePostList(_ context.Context, _ int64, ids []int64) error {
	f.ids = append([]int64(nil), ids...)
	return nil
}

func postTestRouter(reader *postReaderFake, access *postAccessFake) *gin.Engine {
	h := &handler{svc: &Service{Reader: reader, Access: access}}
	r := gin.New()
	w := caller{tenantID: 1}
	r.GET("/page", func(c *gin.Context) { h.postPage(c, w) })
	r.GET("/get", func(c *gin.Context) { h.postGet(c, w) })
	r.GET("/export-excel", func(c *gin.Context) { h.postExport(c, w) })
	r.DELETE("/delete-list", func(c *gin.Context) { h.postDeleteList(c, w) })
	return r
}

func postCall(r http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestPostPageAndGetHTTPContract(t *testing.T) {
	reader := &postReaderFake{page: Page[PostDetail]{List: []PostDetail{{ID: 17, Name: "研发", Code: "dev", Sort: 3, Status: 0, Remark: "备注", CreateTime: 1700000000000}}, Total: 1}}
	r := postTestRouter(reader, &postAccessFake{})
	w := postCall(r, http.MethodGet, "/page?code=de&name=%E7%A0%94&status=0&pageNo=2&pageSize=20")
	var result struct {
		Code int `json:"code"`
		Data struct {
			List  []PostDetail `json:"list"`
			Total int64        `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 0 || result.Data.Total != 1 || len(result.Data.List) != 1 || result.Data.List[0].CreateTime != 1700000000000 {
		t.Fatalf("分页响应不完整：%s, %v", w.Body.String(), err)
	}
	if reader.query.PageNo != 2 || reader.query.PageSize != 20 || reader.query.Code != "de" || reader.query.Name != "研" || reader.query.Status == nil || *reader.query.Status != 0 {
		t.Fatalf("过滤条件丢失：%+v", reader.query)
	}
	for _, path := range []string{"/page?pageNo=0", "/page?pageNo=", "/page?pageSize=201", "/page?pageSize=", "/page?status=abc"} {
		w := postCall(r, http.MethodGet, path)
		if !bytes.Contains(w.Body.Bytes(), []byte(`"code":400`)) {
			t.Fatalf("非法分页参数未拒绝：%s => %s", path, w.Body.String())
		}
	}
	if reader.pageCalls != 1 {
		t.Fatalf("非法请求进入存储层：%d", reader.pageCalls)
	}
	w = postCall(r, http.MethodGet, "/get?id=99")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"data":null`)) {
		t.Fatalf("缺失岗位应返回 null：%s", w.Body.String())
	}
	reader.post = &PostDetail{ID: 17, Name: "研发", CreateTime: 1700000000000}
	w = postCall(r, http.MethodGet, "/get?id=17")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"createTime":1700000000000`)) {
		t.Fatalf("详情缺失创建时间：%s", w.Body.String())
	}
}

func TestPostDeleteListAndExportHTTPContract(t *testing.T) {
	reader := &postReaderFake{
		page:   Page[PostDetail]{List: []PostDetail{{ID: 9007199254740993, Name: "研发", Code: "dev", Sort: 3, Status: 0}}, Total: 1},
		labels: map[int]string{0: "开启"},
	}
	access := &postAccessFake{}
	r := postTestRouter(reader, access)
	w := postCall(r, http.MethodDelete, "/delete-list?ids=7,8&ids=9")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"data":true`)) || !reflect.DeepEqual(access.ids, []int64{7, 8, 9}) {
		t.Fatalf("批量删除解析错误：%s ids=%v", w.Body.String(), access.ids)
	}
	w = postCall(r, http.MethodDelete, "/delete-list")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"code":400`)) {
		t.Fatalf("缺失 ids 未拒绝：%s", w.Body.String())
	}
	w = postCall(r, http.MethodGet, "/export-excel?name=%E7%A0%94")
	if w.Code != http.StatusOK || !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) ||
		w.Header().Get("Content-Type") != "application/vnd.ms-excel;charset=UTF-8" ||
		w.Header().Get("Content-Disposition") != "attachment;filename="+url.QueryEscape("岗位数据.xls") {
		t.Fatalf("导出响应头或 XLSX 内容不符：HTTP %d, type=%q, disposition=%q",
			w.Code, w.Header().Get("Content-Type"), w.Header().Get("Content-Disposition"))
	}
	if reader.query.PageSize != -1 || reader.query.Name != "研" || reader.labelCalls != 1 {
		t.Fatalf("导出未执行无分页筛选和字典转换：query=%+v labels=%d", reader.query, reader.labelCalls)
	}
}
