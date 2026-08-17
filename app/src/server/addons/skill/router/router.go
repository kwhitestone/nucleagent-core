// Package skill 的 HTTP 路由：Skill 列表（只读）。
package router

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"
	"github.com/kwhitestone/prism-fusion/global"
)

// RegisterRoutes 注册 Skill 路由（技能列表只读 + 技能绑定 CRUD）。
func RegisterRoutes(api huma.API) {
	registerListSkills(api)
	registerListBindings(api)
	registerCreateBinding(api)
	registerDeleteBinding(api)
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

// ===== Skill Bindings CRUD =====
//
// SkillBinding 把某个 Skill 安装到 template/instance/conversation。绑定是全局
// 资源（ownerType+ownerID 标识归属实体，不按 user_id 过滤）。

// ---- List ----

// SkillBindingsOutputBody 绑定列表响应信封。
type SkillBindingsOutputBody struct {
	Code    int                   `json:"code" example:"0"`
	Message string                `json:"message" example:"success"`
	Data    []*model.SkillBinding `json:"data"`
}

type bindingsOutput struct {
	Body SkillBindingsOutputBody
}

func registerListBindings(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "skillListBindings",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/skill/bindings",
		Summary:     "列出技能绑定",
		Description: "可选按 ownerType/ownerId 过滤（query 参数）",
		Tags:        []string{"Skill"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *struct {
		OwnerType string `query:"ownerType" doc:"归属类型(template/instance/conversation)"`
		OwnerID   uint   `query:"ownerId" doc:"归属ID"`
	}) (*bindingsOutput, error) {
		var bindings []*model.SkillBinding
		q := global.PRISM_DB.Model(&model.SkillBinding{})
		if input.OwnerType != "" {
			q = q.Where("owner_type = ?", input.OwnerType)
		}
		if input.OwnerID > 0 {
			q = q.Where("owner_id = ?", input.OwnerID)
		}
		q.Order("id asc").Find(&bindings)
		out := &bindingsOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = bindings
		return out, nil
	})
}

// ---- Create ----

type bindingCreateBody struct {
	OwnerType   string `json:"ownerType" doc:"归属类型(template/instance/conversation)"`
	OwnerID     uint   `json:"ownerId" doc:"归属ID"`
	SkillID     uint   `json:"skillId" doc:"技能ID"`
	InstallPath string `json:"installPath,omitempty" maxLength:"512" doc:"本地安装路径(Executor使用)"`
}

type bindingCreateInput struct {
	Body bindingCreateBody
}

// SkillBindingCreateOutputBody 创建响应信封。
type SkillBindingCreateOutputBody struct {
	Code    int                 `json:"code" example:"0"`
	Message string              `json:"message" example:"created"`
	Data    *model.SkillBinding `json:"data"`
}

type bindingCreateOutput struct {
	Body SkillBindingCreateOutputBody
}

func registerCreateBinding(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "skillCreateBinding",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/skill/bindings",
		Summary:     "创建技能绑定（skill → owner）",
		Tags:        []string{"Skill"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *bindingCreateInput) (*bindingCreateOutput, error) {
		if input.Body.OwnerType == "" || input.Body.OwnerID == 0 || input.Body.SkillID == 0 {
			return nil, huma.NewError(http.StatusBadRequest, "ownerType/ownerId/skillId 不能为空")
		}
		// 校验技能存在。
		var skill model.Skill
		if err := global.PRISM_DB.First(&skill, input.Body.SkillID).Error; err != nil {
			return nil, huma.NewError(http.StatusBadRequest, "技能不存在")
		}
		binding := &model.SkillBinding{
			OwnerType:   input.Body.OwnerType,
			OwnerID:     input.Body.OwnerID,
			SkillID:     input.Body.SkillID,
			InstallPath: input.Body.InstallPath,
		}
		if err := global.PRISM_DB.Create(binding).Error; err != nil {
			global.PRISM_LOG.Error("skill: 创建绑定失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "创建绑定失败")
		}
		out := &bindingCreateOutput{}
		out.Body.Code = 0
		out.Body.Message = "created"
		out.Body.Data = binding
		return out, nil
	})
}

// ---- Delete ----

type bindingDeleteInput struct {
	ID string `path:"id"`
}

type bindingDeleteOutput struct {
	Body struct {
		Code    int    `json:"code" example:"0"`
		Message string `json:"message" example:"deleted"`
	}
}

func registerDeleteBinding(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "skillDeleteBinding",
		Method:      http.MethodDelete,
		Path:        "/api/v1/addons/skill/bindings/{id}",
		Summary:     "删除技能绑定",
		Tags:        []string{"Skill"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *bindingDeleteInput) (*bindingDeleteOutput, error) {
		id, err := strconv.ParseUint(input.ID, 10, 64)
		if err != nil {
			return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
		}
		var binding model.SkillBinding
		if err := global.PRISM_DB.First(&binding, id).Error; err != nil {
			return nil, huma.NewError(http.StatusNotFound, "绑定不存在")
		}
		if err := global.PRISM_DB.Delete(&binding).Error; err != nil {
			global.PRISM_LOG.Error("skill: 删除绑定失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "删除绑定失败")
		}
		out := &bindingDeleteOutput{}
		out.Body.Code = 0
		out.Body.Message = "deleted"
		return out, nil
	})
}
