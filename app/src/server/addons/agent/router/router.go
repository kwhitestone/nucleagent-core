// Package agent 的 HTTP 路由：Agent 模板列表（只读）。
package router

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"
	"whitestone.top/prism-fusion/global"
)

// RegisterRoutes 注册 Agent 模板路由。
func RegisterRoutes(api huma.API) {
	registerListTemplates(api)
}

// TemplatesInput 列出 Agent 模板（无入参）。
type TemplatesInput struct{}

// TemplatesOutput 列表响应，沿用框架的 { code, message, data } 数字信封。
// huma 用匿名结构体名生成 schema，必须全局唯一，因此命名为 AgentTemplatesOutputBody。
type AgentTemplatesOutputBody struct {
	Code    int                    `json:"code" example:"0"`
	Message string                 `json:"message" example:"success"`
	Data    []*model.AgentTemplate `json:"data"`
}

type TemplatesOutput struct {
	Body AgentTemplatesOutputBody
}

func registerListTemplates(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "agentListTemplates",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/agent/templates",
		Summary:     "列出 Agent 模板",
		Description: "返回所有启用的 Agent 模板，供前端创作/任务视图选择",
		Tags:        []string{"Agent"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, _ *TemplatesInput) (*TemplatesOutput, error) {
		var templates []*model.AgentTemplate
		// 只返回启用的模板，按 ID 升序保持 seed 顺序。
		if err := global.PRISM_DB.Where("is_active = ?", true).
			Order("id asc").Find(&templates).Error; err != nil {
			global.PRISM_LOG.Error("agent: 查询模板失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "查询模板失败")
		}
		out := &TemplatesOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = templates
		return out, nil
	})
}
