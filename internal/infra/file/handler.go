package file

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weilhuang/yudao-cloud-go/internal/platform/httpx"
	"github.com/weilhuang/yudao-cloud-go/internal/system/auth"
)

const (
	defaultMaxFileBytes    int64 = 16 << 20
	defaultMaxRequestBytes int64 = 32 << 20
)

// UploadLimits 对齐 Java 的 multipart 限制。零值沿用 16/32 MiB 默认值；
// 服务启动时还会校验显式配置，避免把零值误当成无限制。
type UploadLimits struct {
	MaxFileBytes    int64
	MaxRequestBytes int64
}

func (l UploadLimits) effective() UploadLimits {
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = defaultMaxFileBytes
	}
	if l.MaxRequestBytes == 0 {
		l.MaxRequestBytes = defaultMaxRequestBytes
	}
	return l
}

// Mount 挂上文件上传、文件记录和文件配置。下载接口不要求登录，和 Java 的 PermitAll 一致。
func Mount(r *gin.Engine, sessions *auth.Service, svc *Service, limits UploadLimits) {
	h := &handler{sessions: sessions, svc: svc, limits: limits.effective()}
	files := r.Group("/admin-api/infra/file")
	files.POST("/upload", h.login(h.upload))
	files.GET("/presigned-url", h.login(h.presign))
	files.POST("/create", h.login(h.create))
	files.GET("/page", h.permit("infra:file:query", h.page))
	files.GET("/get", h.permit("infra:file:query", h.get))
	files.DELETE("/delete", h.permit("infra:file:delete", h.delete))
	files.DELETE("/delete-list", h.permit("infra:file:delete", h.deleteList))
	files.GET("/:configId/get/*objectPath", h.download)
	appFiles := r.Group("/app-api/infra/file")
	appFiles.POST("/upload", h.appLogin(h.upload))
	appFiles.GET("/presigned-url", h.appLogin(h.presign))
	appFiles.POST("/create", h.appLogin(h.create))

	cfg := r.Group("/admin-api/infra/file-config")
	cfg.POST("/create", h.permit("infra:file-config:create", h.configSave))
	cfg.PUT("/update", h.permit("infra:file-config:update", h.configSave))
	cfg.PUT("/update-master", h.permit("infra:file-config:update", h.configMaster))
	cfg.DELETE("/delete", h.permit("infra:file-config:delete", h.configDelete))
	cfg.DELETE("/delete-list", h.permit("infra:file-config:delete", h.configDeleteList))
	cfg.GET("/get", h.permit("infra:file-config:query", h.configGet))
	cfg.GET("/page", h.permit("infra:file-config:query", h.configPage))
	cfg.GET("/test", h.permit("infra:file-config:query", h.configTest))
}

type handler struct {
	sessions *auth.Service
	svc      *Service
	limits   UploadLimits
}

type caller struct {
	userID   int64
	tenantID int64
}

func (h *handler) login(next func(*gin.Context, caller)) gin.HandlerFunc {
	return h.permit("", next)
}

func (h *handler) permit(perm string, next func(*gin.Context, caller)) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, tenantID, perms, err := h.sessions.Session(c.Request.Context(), bearer(c))
		if err != nil {
			writeAuth(c, err)
			return
		}
		header, ok := headerTenant(c)
		if !ok || header != tenantID {
			msg := "请求的租户标识未传递，请进行排查"
			code := 400
			if ok {
				msg = "您无权访问该租户的数据"
				code = 403
			}
			httpx.Fail(c, http.StatusOK, code, msg)
			return
		}
		if perm != "" && !perms[perm] {
			httpx.Fail(c, http.StatusOK, 403, "没有该操作权限")
			return
		}
		next(c, caller{tenantID: tenantID})
	}
}

// appLogin 给 App 文件接口用。会员和管理员令牌都可以，服务令牌不能访问。
func (h *handler) appLogin(next func(*gin.Context, caller)) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := h.sessions.Check(c.Request.Context(), bearer(c))
		if err != nil {
			writeAuth(c, err)
			return
		}
		if (token.UserType != 1 && token.UserType != 2) || token.UserID <= 0 {
			httpx.Fail(c, http.StatusOK, 403, "没有该操作权限")
			return
		}
		header, ok := headerTenant(c)
		if !ok || header != token.TenantID {
			msg := "请求的租户标识未传递，请进行排查"
			code := 400
			if ok {
				msg = "您无权访问该租户的数据"
				code = 403
			}
			httpx.Fail(c, http.StatusOK, code, msg)
			return
		}
		next(c, caller{userID: token.UserID, tenantID: token.TenantID})
	}
}

func (h *handler) upload(c *gin.Context, _ caller) {
	// 总请求上限包含 multipart 边界、目录等字段；不能只检查文件大小。
	if c.Request.ContentLength > h.limits.MaxRequestBytes {
		writeUploadTooLarge(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.limits.MaxRequestBytes)
	// Gin 默认会在内存中缓存 32 MiB 文件；这里最多缓存 8 MiB，剩余落盘并在返回前清理。
	memoryBudget := min(h.limits.MaxFileBytes, int64(8<<20))
	if err := c.Request.ParseMultipartForm(memoryBudget); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeUploadTooLarge(c)
		} else {
			writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		}
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	header, err := c.FormFile("file")
	if err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if header.Size > h.limits.MaxFileBytes {
		writeUploadTooLarge(c)
		return
	}
	opened, err := header.Open()
	if err != nil {
		writeBiz(c, err)
		return
	}
	defer opened.Close()
	// 即使文件头大小异常，读取也只能多取一字节用于判断超限。
	content, err := io.ReadAll(io.LimitReader(opened, h.limits.MaxFileBytes+1))
	if err != nil {
		writeBiz(c, err)
		return
	}
	if int64(len(content)) > h.limits.MaxFileBytes {
		writeUploadTooLarge(c)
		return
	}
	url, err := h.svc.Upload(c.Request.Context(), content, header.Filename, c.PostForm("directory"), header.Header.Get("Content-Type"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, url)
}

func writeUploadTooLarge(c *gin.Context) {
	// Java GlobalExceptionHandler 对 MaxUploadSizeExceededException 使用 HTTP 200 + 业务码 400。
	httpx.Fail(c, http.StatusOK, 400, "上传文件过大，请调整后重试")
}

func (h *handler) presign(c *gin.Context, _ caller) {
	item, err := h.svc.PresignPut(c.Request.Context(), c.Query("name"), c.Query("directory"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *handler) create(c *gin.Context, _ caller) {
	var item Item
	if err := c.ShouldBindJSON(&item); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	id, err := h.svc.CreateRecord(c.Request.Context(), item)
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, id)
}

func (h *handler) page(c *gin.Context, _ caller) {
	page, err := h.svc.Store.FilePage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("path"), c.Query("type"))
	writePage(c, page, err)
}

func (h *handler) get(c *gin.Context, _ caller) {
	item, err := h.svc.Store.FileByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) delete(c *gin.Context, _ caller) {
	if err := h.svc.Delete(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) deleteList(c *gin.Context, _ caller) {
	for _, id := range queryIDs(c) {
		if err := h.svc.Delete(c.Request.Context(), id); err != nil {
			writeBiz(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

func (h *handler) download(c *gin.Context) {
	configID, _ := strconv.ParseInt(c.Param("configId"), 10, 64)
	// Gin 的通配参数已解码一次；从原始 URL 取路径，才不会把 %252F 再解成 /。
	rawPrefix := "/admin-api/infra/file/" + c.Param("configId") + "/get/"
	rawPath := c.Request.URL.EscapedPath()
	if !strings.HasPrefix(rawPath, rawPrefix) {
		httpx.Fail(c, http.StatusOK, 400, "文件路径不正确")
		return
	}
	objectPath := decodeURLPath(strings.TrimPrefix(rawPath, rawPrefix))
	if objectPath == "" {
		httpx.Fail(c, http.StatusOK, 400, "结尾的 path 路径必须传递")
		return
	}
	content, item, err := h.svc.Content(c.Request.Context(), configID, objectPath)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if content == nil {
		c.Status(http.StatusNotFound)
		return
	}
	name := path.Base(objectPath)
	if item != nil && item.Name != "" {
		name = item.Name
	}
	writeDownload(c, content, name)
}

func (h *handler) configSave(c *gin.Context, _ caller) {
	var item Config
	if err := c.ShouldBindJSON(&item); err != nil {
		writeBiz(c, &Error{Code: 400, Msg: "请求参数不正确"})
		return
	}
	if c.Request.Method == http.MethodPost {
		item.ID = 0
	}
	id, err := h.svc.SaveConfig(c.Request.Context(), item)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if c.Request.Method == http.MethodPost {
		httpx.OK(c, id)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) configMaster(c *gin.Context, _ caller) {
	id := queryInt(c, "id")
	current, err := h.svc.Store.ConfigByID(c.Request.Context(), id)
	if err != nil {
		writeBiz(c, err)
		return
	}
	if current == nil {
		writeBiz(c, &Error{Code: 1_001_006_000, Msg: "文件配置不存在"})
		return
	}
	if err := h.svc.Store.UpdateMaster(c.Request.Context(), id); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) configDelete(c *gin.Context, _ caller) {
	if err := h.svc.DeleteConfig(c.Request.Context(), queryInt(c, "id")); err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, true)
}

func (h *handler) configDeleteList(c *gin.Context, _ caller) {
	for _, id := range queryIDs(c) {
		if err := h.svc.DeleteConfig(c.Request.Context(), id); err != nil {
			writeBiz(c, err)
			return
		}
	}
	httpx.OK(c, true)
}

func (h *handler) configGet(c *gin.Context, _ caller) {
	item, err := h.svc.Store.ConfigByID(c.Request.Context(), queryInt(c, "id"))
	writeOne(c, item, err)
}

func (h *handler) configPage(c *gin.Context, _ caller) {
	var storage *int
	if text := c.Query("storage"); text != "" {
		value := atoi(text, 0)
		storage = &value
	}
	page, err := h.svc.Store.ConfigPage(c.Request.Context(), atoi(c.Query("pageNo"), 1), atoi(c.Query("pageSize"), 10), c.Query("name"), storage)
	writePage(c, page, err)
}

func (h *handler) configTest(c *gin.Context, _ caller) {
	url, err := h.svc.Test(c.Request.Context(), queryInt(c, "id"))
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, url)
}

func writePage[T any](c *gin.Context, page Page[T], err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	if page.List == nil {
		page.List = []T{}
	}
	httpx.OK(c, page)
}

func writeOne[T any](c *gin.Context, item *T, err error) {
	if err != nil {
		writeBiz(c, err)
		return
	}
	httpx.OK(c, item)
}

func writeBiz(c *gin.Context, err error) {
	if biz, ok := err.(*Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	writeAuth(c, err)
}

func writeAuth(c *gin.Context, err error) {
	if biz, ok := err.(*auth.Error); ok {
		httpx.Fail(c, http.StatusOK, biz.Code, biz.Msg)
		return
	}
	httpx.Fail(c, http.StatusOK, 500, "系统异常")
}

func bearer(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if len(header) > 7 && header[:7] == "Bearer " {
		return header[7:]
	}
	return ""
}

func headerTenant(c *gin.Context) (int64, bool) {
	text := c.GetHeader("tenant-id")
	if text == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(text, 10, 64)
	return id, err == nil
}

func queryInt(c *gin.Context, name string) int64 {
	n, _ := strconv.ParseInt(c.Query(name), 10, 64)
	return n
}

func queryIDs(c *gin.Context) []int64 {
	var ids []int64
	for _, text := range c.QueryArray("ids") {
		for _, part := range strings.Split(text, ",") {
			if part == "" {
				continue
			}
			ids = append(ids, int64(atoi(part, 0)))
		}
	}
	return ids
}

func atoi(text string, fallback int) int {
	if text == "" {
		return fallback
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return fallback
	}
	return n
}
