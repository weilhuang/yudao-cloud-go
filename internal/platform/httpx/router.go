package httpx

import "github.com/gin-gonic/gin"

// NewRouter 组装所有进程都有的外壳。业务路由在后续迭代挂到这个 Engine 上。
func NewRouter(appName string) *gin.Engine {
	r := gin.New()
	r.Use(Recover(), RequestID(), CORS(), AccessLog())
	r.GET("/health", func(c *gin.Context) {
		OK(c, gin.H{"status": "up", "name": appName})
	})
	return r
}
