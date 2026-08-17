// Package tool 工具 CRUD 插件。
//
// 暴露 Tool 的增删改查端点，供前端管理工具元数据。Tool 是全局资源（不按
// user_id 过滤）。表由 coredata 插件 AutoMigrate，这里读写。
package tool

import (
	toolRouter "nucleagent-core/addons/tool/router"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// ToolPlugin Tool 插件。
type ToolPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ToolPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "tool",
			PluginDescription: "Tool CRUD - 工具元数据管理",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *ToolPlugin) RoutePrefix() string {
	return "/api/v1/addons/tool"
}

// Priority 在 coredata(迁移/seed) 之后执行。
func (p *ToolPlugin) Priority() int { return 34 }

// RegisterRoutes 注册 Tool 路由。
func (p *ToolPlugin) RegisterRoutes(api huma.API) {
	toolRouter.RegisterRoutes(api)
	global.PRISM_LOG.Info("Tool plugin routes registered")
}

// Models 表由 coredata 统一 AutoMigrate，这里返回 nil 避免重复迁移。
func (p *ToolPlugin) Models() []interface{} {
	return nil
}
