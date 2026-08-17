// Package project 项目 CRUD 插件。
//
// 暴露 Project 的增删改查端点，供前端管理项目。项目按 user_id 隔离（用户只见
// 自己的项目）。表由 coredata 插件 AutoMigrate，这里读写。
package project

import (
	projectRouter "nucleagent-core/addons/project/router"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// ProjectPlugin Project 插件。
type ProjectPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ProjectPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "project",
			PluginDescription: "Project CRUD - 项目管理（按 user_id 隔离）",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *ProjectPlugin) RoutePrefix() string {
	return "/api/v1/addons/project"
}

// Priority 在 coredata(迁移/seed) 之后执行。
func (p *ProjectPlugin) Priority() int { return 33 }

// RegisterRoutes 注册 Project 路由。
func (p *ProjectPlugin) RegisterRoutes(api huma.API) {
	projectRouter.RegisterRoutes(api)
	global.PRISM_LOG.Info("Project plugin routes registered")
}

// Models 表由 coredata 统一 AutoMigrate，这里返回 nil 避免重复迁移。
func (p *ProjectPlugin) Models() []interface{} {
	return nil
}

// Middlewares 作用域中间件：把 gin context 的 user_id 桥接到 request context，
// 供 huma handler 读取（与 conversation 插件同模式）。
func (p *ProjectPlugin) Middlewares() []gin.HandlerFunc {
	return []gin.HandlerFunc{BridgeMiddleware()}
}
