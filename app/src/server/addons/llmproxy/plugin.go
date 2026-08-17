// Package llmproxy 的插件注册 + LLM 配置拉取端点。
package llmproxy

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// LLMProxyPlugin LLM 代理插件。
type LLMProxyPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&LLMProxyPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "llm-proxy",
			PluginDescription: "LLM 代理 - TempLLMKey 签发 + 反向代理到真实 LLM + CallLog",
		},
	})
}

func (p *LLMProxyPlugin) Priority() int { return 30 }

// RoutePrefix 用 /api/llm-proxy（不在 /api/v1/addons 下，避开 JWT 作用域）。
func (p *LLMProxyPlugin) RoutePrefix() string { return "/api/llm-proxy" }

// RegisterRoutes 注册 Proxy 反向代理路由 + LLM 配置拉取端点。
func (p *LLMProxyPlugin) RegisterRoutes(api huma.API) {
	RegisterRoutes(api)
	registerLLMConfigEndpoint(api)
}

// Models Provider 表由 provider 插件迁移（llmproxy 不重复迁移）。
func (p *LLMProxyPlugin) Models() []interface{} { return nil }

// registerLLMConfigEndpoint 注册 GET /api/v1/addons/s2s/llm/config（executor 拉 LLM 配置）。
//
// executor 用此端点获知 provider/model/base_url（不含 API key）。
// 鉴权走 X-Executor-Token（S2S）。
func registerLLMConfigEndpoint(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "s2sLLMConfig",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/s2s/llm/config",
		Summary:     "获取 LLM 配置（S2S）",
		Description: "executor 拉取 LLM 配置（provider/model/base_url，不含 API key）",
		Tags:        []string{"S2S"},
	}, func(ctx context.Context, input *struct{}) (*LLMConfigOutput, error) {
		// v1 占位：返回空配置。真实实现由 conversation 签发 TempLLMKey 时携带，
		// executor 直接用 key 走 Proxy，不需要单独拉 config。
		resp := &LLMConfigOutput{}
		resp.Body.Code = 0
		resp.Body.Message = "ok"
		return resp, nil
	})
}

// LLMConfigOutput LLM 配置响应。
type LLMConfigOutput struct {
	Body struct {
		Code    int             `json:"code" example:"0"`
		Message string          `json:"message" example:"ok"`
		Data    *llm.TempLLMKey `json:"data,omitempty"`
	}
}
