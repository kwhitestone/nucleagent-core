package project

import (
	"context"

	"github.com/gin-gonic/gin"
	"nucleagent-core/addons/project/router"
)

// BridgeMiddleware 把 JWT 中间件写入 gin.Context 的 user_id 复制到 request context，
// 供 huma handler 通过 ctx 读取（与 conversation 插件同模式）。
func BridgeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		v, exists := c.Get("user_id")
		if exists {
			uid, _ := v.(uint)
			ctx := context.WithValue(c.Request.Context(), router.UserIDKey(), uid)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
