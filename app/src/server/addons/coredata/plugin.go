// Package coredata 核心数据插件：迁移 conversation/executorreg/llmproxy 之外的表 + seed。
//
// 把 Provider/AgentTemplate/AgentInstance/Skill/SkillBinding/Tool/CallLog/Project
// 集中迁移（各业务 CRUD addon 后置；这些表先存在供 conversation 编排用）。
// 同时 seed 默认 Provider + AgentTemplate。
package coredata

import (
	"encoding/json"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/model"
	"nucleagent-core/addons/llmproxy"
	"whitestone.top/prism-fusion/global"
	"whitestone.top/prism-fusion/plugin"
)

// CoreDataPlugin 核心数据迁移 + seed 插件。
type CoreDataPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&CoreDataPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "coredata",
			PluginDescription: "核心数据迁移 + seed（Provider/Agent/Skill/Tool/CallLog/Project）",
		},
	})
}

func (p *CoreDataPlugin) Priority() int { return 15 } // 在 conversation(40) 之前迁移

func (p *CoreDataPlugin) RoutePrefix() string { return "/api/v1/addons/coredata" }

func (p *CoreDataPlugin) RegisterRoutes(api huma.API) {
	// 路由由各业务 addon 提供；此处仅做 seed。
	seedDefaults()
	global.PRISM_LOG.Info("CoreData plugin seeded")
}

// Models 暴露核心表供框架 AutoMigrate。
func (p *CoreDataPlugin) Models() []interface{} {
	return []interface{}{
		&model.Provider{},
		&model.AgentTemplate{},
		&model.AgentInstance{},
		&model.Skill{},
		&model.SkillBinding{},
		&model.Tool{},
		&model.CallLog{},
		&model.Project{},
	}
}

// seedDefaults seed 默认 Provider（OpenAI 兼容）+ 默认 AgentTemplate。
//
// Provider.api_key 用 MASTER_KEY 加密；真实 key 从环境变量 OPENAI_API_KEY 读，
// 未设置则用占位（仅 dev 用）。
func seedDefaults() {
	db := global.PRISM_DB
	if db == nil {
		return
	}

	// seed Provider（仅当表空）。
	var providerCount int64
	db.Model(&model.Provider{}).Count(&providerCount)
	if providerCount == 0 {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			apiKey = "sk-placeholder-set-OPENAI_API_KEY"
		}
		encKey, err := encryptAPIKeySafe(apiKey)
		if err != nil {
			global.PRISM_LOG.Warn("coredata: encrypt api key failed, storing plaintext (dev only)", )
			encKey = apiKey
		}
		cfg, _ := json.Marshal(map[string]any{
			"baseUrl":    envDefault("OPENAI_BASE_URL", "https://api.openai.com"),
			"apiFormat":  "openai",
			"authScheme": "bearer",
			"models":     []string{"gpt-4o-mini", "gpt-4o"},
		})
		provider := &model.Provider{
			Name:     "OpenAI",
			APIKey:   encKey,
			Config:   model.JSON(cfg),
			IsActive: true,
		}
		db.Create(provider)
		global.PRISM_LOG.Info("coredata: seeded default Provider 'OpenAI'")
	}

	// seed AgentTemplate（仅当表空）。
	var tplCount int64
	db.Model(&model.AgentTemplate{}).Count(&tplCount)
	if tplCount == 0 {
		tpl := &model.AgentTemplate{
			Name:     "通用助手",
			Slug:     "general-assistant",
			IsActive: true,
		}
		cfg, _ := json.Marshal(map[string]any{
			"category":    "general",
			"role":        "通用 AI 助手",
			"personality": "专业、简洁、友好",
			"prompt":      "你是一个通用 AI 助手，帮助用户完成各类任务。",
			"sort_order":  0,
		})
		tpl.Config = model.JSON(cfg)
		i18n, _ := json.Marshal(map[string]any{
			"zh": map[string]string{"name": "通用助手", "personality": "专业、简洁、友好"},
			"en": map[string]string{"name": "General Assistant", "personality": "Professional, concise, friendly"},
		})
		tpl.I18n = model.JSON(i18n)
		db.Create(tpl)
		global.PRISM_LOG.Info("coredata: seeded default AgentTemplate 'general-assistant'")
	}
}

// encryptAPIKeySafe 安全包装加密（MASTER_KEY 未设时返回 error）。
func encryptAPIKeySafe(plain string) (string, error) {
	return llmproxy.EncryptAPIKey(plain)
}

// envDefault 返回环境变量值，空则返回 fallback。
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
