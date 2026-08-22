// Package conversation 对话插件：Conversation/Message/Step CRUD + SSE 流。
//
// 通过 Models() 暴露三张表供框架 AutoMigrate；路由注册 + 把编排服务注入 executorreg。
package conversation

import (
	convRouter "nucleagent-core/addons/conversation/router"
	"nucleagent-core/addons/conversation/svc"
	"nucleagent-core/addons/executorreg"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"github.com/nucleagent/nucleagent-shared/model"
	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// ConversationPlugin 对话插件
type ConversationPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ConversationPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "conversation",
			PluginDescription: "对话插件 - Conversation/Message/Step CRUD + SSE 流 + A2A 编排",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *ConversationPlugin) RoutePrefix() string {
	return "/api/v1/addons/conversation"
}

// Priority 在 executor-reg(25) 之后执行，确保注册时 executorreg.SetHandler 可用。
func (p *ConversationPlugin) Priority() int { return 40 }

// RegisterRoutes 注册对话路由 + 注入编排服务到 executorreg（接收 executor 上报）。
func (p *ConversationPlugin) RegisterRoutes(api huma.API) {
	// 把对话服务注入 executorreg，作为 executor 上报信封的 handler。
	executorreg.SetHandler(svc.Default)
	// 注入带外续轮开启回调（delegate_task 后台完成后的 turn 2 执行上下文）。
	executorreg.SetAsyncContinuationStarter(svc.Default)
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

// Middlewares 作用域中间件：仅对 /api/v1/addons/conversation 前缀生效。
// BridgeMiddleware 把 gin context 的 user_id 桥接到 request context，供 huma handler 读取。
func (p *ConversationPlugin) Middlewares() []gin.HandlerFunc {
	return []gin.HandlerFunc{BridgeMiddleware()}
}
