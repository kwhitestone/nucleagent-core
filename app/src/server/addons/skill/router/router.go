// Package skill 的 HTTP 路由：Skill 列表（只读）。
package router

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"
	"whitestone.top/prism-fusion/global"
)

// RegisterRoutes 注册 Skill 路由。
func RegisterRoutes(api huma.API) {
	registerListSkills(api)
}

// ListInput 列出技能（无入参）。
type ListInput struct{}

// ListOutput 列表响应，沿用 { code, message, data } 数字信封。
// huma 用匿名结构体名生成 schema，必须全局唯一，因此命名为 SkillListOutputBody。
type SkillListOutputBody struct {
	Code    int            `json:"code" example:"0"`
	Message string         `json:"message" example:"success"`
	Data    []*model.Skill `json:"data"`
}

type ListOutput struct {
	Body SkillListOutputBody
}

func registerListSkills(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "skillList",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/skill",
		Summary:     "列出技能",
		Description: "返回所有启用的技能",
		Tags:        []string{"Skill"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, _ *ListInput) (*ListOutput, error) {
		var skills []*model.Skill
		if err := global.PRISM_DB.Where("is_active = ?", true).
			Order("id asc").Find(&skills).Error; err != nil {
			global.PRISM_LOG.Error("skill: 查询技能失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "查询技能失败")
		}
		out := &ListOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = skills
		return out, nil
	})
}
