// Package agent Agent 模板（只读）+ Agent 实例（CRUD）插件。
//
// 暴露：
//   - GET /api/v1/addons/agent/templates      列出模板（全局只读）
//   - POST/GET/GET/PATCH/DELETE .../instances  雇佣/列出/详情/更新/解雇 Agent（按 user_id 隔离）
//
// 模板表由 coredata 插件 AutoMigrate + seed。实例表同由 coredata 迁移。
package agent

import (
	agentRouter "nucleagent-core/addons/agent/router"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// AgentPlugin Agent 模板插件。
type AgentPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&AgentPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "agent",
			PluginDescription: "Agent 模板只读 - 列出可用模板",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *AgentPlugin) RoutePrefix() string {
	return "/api/v1/addons/agent"
}

// Priority 在 coredata(迁移/seed) 之后执行。
func (p *AgentPlugin) Priority() int { return 30 }

// RegisterRoutes 注册 Agent 模板路由。
func (p *AgentPlugin) RegisterRoutes(api huma.API) {
	agentRouter.RegisterRoutes(api)
	global.PRISM_LOG.Info("Agent plugin routes registered")
}

// Models 表由 coredata 统一 AutoMigrate，这里返回 nil 避免重复迁移。
func (p *AgentPlugin) Models() []interface{} {
	return nil
}

// Middlewares 作用域中间件：把 gin context 的 user_id 桥接到 request context，
// 供实例 CRUD handler 读取（模板列表是全局只读，忽略 user_id）。
func (p *AgentPlugin) Middlewares() []gin.HandlerFunc {
	return []gin.HandlerFunc{BridgeMiddleware()}
}
