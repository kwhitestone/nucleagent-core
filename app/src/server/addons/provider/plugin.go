// Package provider LLM 提供商 CRUD 插件。
//
// 暴露 Provider 的增删改查端点，供前端管理 LLM 提供商。表由 coredata 插件
// AutoMigrate + seed，这里读写。
//
// 安全要点：Provider.APIKey 字段 json tag 是 "-"，GET 永不回传明文密钥；
// 创建/更新时单独接收明文 apiKey，用 coredata 同款 MASTER_KEY 加密入库。
package provider

import (
	providerRouter "nucleagent-core/addons/provider/router"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// ProviderPlugin Provider 插件。
type ProviderPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ProviderPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "provider",
			PluginDescription: "Provider CRUD - LLM 提供商管理",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *ProviderPlugin) RoutePrefix() string {
	return "/api/v1/addons/provider"
}

// Priority 在 coredata(迁移/seed) 之后执行。
func (p *ProviderPlugin) Priority() int { return 32 }

// RegisterRoutes 注册 Provider 路由。
func (p *ProviderPlugin) RegisterRoutes(api huma.API) {
	providerRouter.RegisterRoutes(api)
	global.PRISM_LOG.Info("Provider plugin routes registered")
}

// Models 表由 coredata 统一 AutoMigrate，这里返回 nil 避免重复迁移。
func (p *ProviderPlugin) Models() []interface{} {
	return nil
}
