// Package skill Skill 只读插件。
//
// 暴露 GET /api/v1/addons/skill，供前端列出可用技能。表由 coredata AutoMigrate，
// 这里只读。与 agent 插件同构。
package skill

import (
	skillRouter "nucleagent-core/addons/skill/router"

	"github.com/danielgtaylor/huma/v2"
	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// SkillPlugin Skill 插件。
type SkillPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&SkillPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "skill",
			PluginDescription: "Skill 只读 - 列出可用技能",
		},
	})
}

// RoutePrefix 业务路由统一挂载在 /api/v1/addons/ 下。
func (p *SkillPlugin) RoutePrefix() string {
	return "/api/v1/addons/skill"
}

// Priority 在 coredata(迁移/seed) 之后执行。
func (p *SkillPlugin) Priority() int { return 31 }

// RegisterRoutes 注册 Skill 路由。
func (p *SkillPlugin) RegisterRoutes(api huma.API) {
	skillRouter.RegisterRoutes(api)
	global.PRISM_LOG.Info("Skill plugin routes registered")
}

// Models 表由 coredata 统一 AutoMigrate，这里返回 nil 避免重复迁移。
func (p *SkillPlugin) Models() []interface{} {
	return nil
}
