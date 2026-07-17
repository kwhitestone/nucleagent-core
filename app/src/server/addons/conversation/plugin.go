// Package conversation 对话插件：Conversation/Message/Step CRUD + SSE 流。
// 骨架阶段：注册路由占位 + 通过 Models() 暴露三张表供框架 AutoMigrate。
package conversation

import (
	convRouter "nucleagent-core/addons/conversation/router"

	"github.com/nucleagent/nucleagent-shared/model"
	"whitestone.top/prism-fusion/global"
	"whitestone.top/prism-fusion/plugin"

	"github.com/danielgtaylor/huma/v2"
)

// ConversationPlugin 对话插件
type ConversationPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ConversationPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "conversation",
			PluginDescription: "对话插件 - Conversation/Message/Step CRUD + SSE 流",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *ConversationPlugin) RoutePrefix() string {
	return "/api/v1/addons/conversation"
}

// RegisterRoutes 注册对话路由（骨架占位，CRUD + SSE 待实现）。
func (p *ConversationPlugin) RegisterRoutes(api huma.API) {
	convRouter.RegisterRoutes(api)
	global.PRISM_LOG.Info("Conversation plugin routes registered")
}

// Models 暴露对话相关表供框架 AutoMigrate。
func (p *ConversationPlugin) Models() []interface{} {
	return []interface{}{
		&model.Conversation{},
		&model.Message{},
		&model.Step{},
	}
}
