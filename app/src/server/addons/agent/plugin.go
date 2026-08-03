// Package agent Agent 模板只读插件。
//
// 暴露 GET /api/v1/addons/agent/templates，供前端「创作」「任务」视图拉取可用
// Agent 模板。表由 coredata 插件 AutoMigrate + seed，这里只读不写。
//
// 为什么单独成包：前端 api/agent.ts 早已在调用此端点，但后端从未实现（重构时
// 发现的缺口）。只补这一个只读端点即可解锁新视图，instance/CRUD 等留待后续。
package agent

import (
	agentRouter "nucleagent-core/addons/agent/router"

	"github.com/danielgtaylor/huma/v2"
	"whitestone.top/prism-fusion/global"
	"whitestone.top/prism-fusion/plugin"
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
