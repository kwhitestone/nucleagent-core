// Package tool 的 HTTP 路由：Tool CRUD。
//
// Tool 是全局资源（不按 user_id 过滤）。slug 唯一；创建/更新时由调用方保证。
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

// RegisterRoutes 注册 Tool 路由。
func RegisterRoutes(api huma.API) {
	registerList(api)
	registerGet(api)
	registerCreate(api)
	registerUpdate(api)
	registerDelete(api)
}

// ---- List ----

// ToolListOutputBody 列表响应信封。
type ToolListOutputBody struct {
	Code    int           `json:"code" example:"0"`
	Message string        `json:"message" example:"success"`
	Data    []*model.Tool `json:"data"`
}

type listOutput struct {
	Body ToolListOutputBody
}

func registerList(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "toolList",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/tool",
		Summary:     "列出工具",
		Tags:        []string{"Tool"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, _ *struct{}) (*listOutput, error) {
		var tools []*model.Tool
		if err := global.PRISM_DB.Order("id asc").Find(&tools).Error; err != nil {
			global.PRISM_LOG.Error("tool: 查询失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "查询失败")
		}
		out := &listOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = tools
		return out, nil
	})
}

// ---- Get ----

// ToolGetOutputBody 详情响应信封。
type ToolGetOutputBody struct {
	Code    int         `json:"code" example:"0"`
	Message string      `json:"message" example:"success"`
	Data    *model.Tool `json:"data"`
}

type getOutput struct {
	Body ToolGetOutputBody
}

func registerGet(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "toolGet",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/tool/{id}",
		Summary:     "工具详情",
		Tags:        []string{"Tool"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *struct {
		ID string `path:"id"`
	}) (*getOutput, error) {
		tool, err := loadTool(input.ID)
		if err != nil {
			return nil, err
		}
		out := &getOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = tool
		return out, nil
	})
}

// ---- Create ----

// config/i18n 用 map[string]any 而非 model.JSON：huma 把 model.JSON（json.RawMessage）
// 生成为 string schema，会拒绝 JSON 对象输入（422）。map 才能正确生成 object schema。
type toolCreateBody struct {
	Name     string         `json:"name" maxLength:"128" doc:"工具名称"`
	Slug     string         `json:"slug" maxLength:"128" doc:"工具唯一标识"`
	Config   map[string]any `json:"config,omitempty" doc:"工具配置(type/description/mcp_config)"`
	I18n     map[string]any `json:"i18n,omitempty" doc:"多语言文案"`
	IsActive bool           `json:"isActive"`
}

type createInput struct {
	Body toolCreateBody
}

// ToolCreateOutputBody 创建响应信封。
type ToolCreateOutputBody struct {
	Code    int         `json:"code" example:"0"`
	Message string      `json:"message" example:"created"`
	Data    *model.Tool `json:"data"`
}

type createOutput struct {
	Body ToolCreateOutputBody
}

func registerCreate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "toolCreate",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/tool",
		Summary:     "创建工具",
		Tags:        []string{"Tool"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *createInput) (*createOutput, error) {
		if input.Body.Name == "" {
			return nil, huma.NewError(http.StatusBadRequest, "name 不能为空")
		}
		if input.Body.Slug == "" {
			return nil, huma.NewError(http.StatusBadRequest, "slug 不能为空")
		}
		tool := &model.Tool{
			Name:     input.Body.Name,
			Slug:     input.Body.Slug,
			Config:   model.MustNewJSON(input.Body.Config),
			I18n:     model.MustNewJSON(input.Body.I18n),
			IsActive: input.Body.IsActive,
		}
		if err := global.PRISM_DB.Create(tool).Error; err != nil {
			global.PRISM_LOG.Error("tool: 创建失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "创建失败（slug 可能重复）")
		}
		out := &createOutput{}
		out.Body.Code = 0
		out.Body.Message = "created"
		out.Body.Data = tool
		return out, nil
	})
}

// ---- Update ----

type toolPatchBody struct {
	Name     *string         `json:"name,omitempty" maxLength:"128"`
	Slug     *string         `json:"slug,omitempty" maxLength:"128"`
	Config   *map[string]any `json:"config,omitempty"`
	I18n     *map[string]any `json:"i18n,omitempty"`
	IsActive *bool           `json:"isActive,omitempty"`
}

type updateInput struct {
	ID   string `path:"id"`
	Body toolPatchBody
}

// ToolUpdateOutputBody 更新响应信封。
type ToolUpdateOutputBody struct {
	Code    int         `json:"code" example:"0"`
	Message string      `json:"message" example:"updated"`
	Data    *model.Tool `json:"data"`
}

type updateOutput struct {
	Body ToolUpdateOutputBody
}

func registerUpdate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "toolUpdate",
		Method:      http.MethodPatch,
		Path:        "/api/v1/addons/tool/{id}",
		Summary:     "更新工具",
		Tags:        []string{"Tool"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *updateInput) (*updateOutput, error) {
		tool, err := loadTool(input.ID)
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{}
		if input.Body.Name != nil {
			updates["name"] = *input.Body.Name
		}
		if input.Body.Slug != nil {
			updates["slug"] = *input.Body.Slug
		}
		if input.Body.Config != nil {
			updates["config"] = model.MustNewJSON(*input.Body.Config)
		}
		if input.Body.I18n != nil {
			updates["i18n"] = model.MustNewJSON(*input.Body.I18n)
		}
		if input.Body.IsActive != nil {
			updates["is_active"] = *input.Body.IsActive
		}
		if len(updates) > 0 {
			if err := global.PRISM_DB.Model(&model.Tool{}).Where("id = ?", tool.ID).Updates(updates).Error; err != nil {
				global.PRISM_LOG.Error("tool: 更新失败", zap.Error(err))
				return nil, huma.NewError(http.StatusInternalServerError, "更新失败（slug 可能重复）")
			}
			global.PRISM_DB.First(tool, tool.ID)
		}
		out := &updateOutput{}
		out.Body.Code = 0
		out.Body.Message = "updated"
		out.Body.Data = tool
		return out, nil
	})
}

// ---- Delete ----

type deleteInput struct {
	ID string `path:"id"`
}

type deleteOutput struct {
	Body struct {
		Code    int    `json:"code" example:"0"`
		Message string `json:"message" example:"deleted"`
	}
}

func registerDelete(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "toolDelete",
		Method:      http.MethodDelete,
		Path:        "/api/v1/addons/tool/{id}",
		Summary:     "删除工具",
		Tags:        []string{"Tool"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *deleteInput) (*deleteOutput, error) {
		tool, err := loadTool(input.ID)
		if err != nil {
			return nil, err
		}
		if err := global.PRISM_DB.Delete(tool).Error; err != nil {
			global.PRISM_LOG.Error("tool: 删除失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "删除失败")
		}
		out := &deleteOutput{}
		out.Body.Code = 0
		out.Body.Message = "deleted"
		return out, nil
	})
}

// loadTool 按 id 加载工具，不存在返回 404。
func loadTool(idStr string) (*model.Tool, error) {
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
	}
	var tool model.Tool
	if err := global.PRISM_DB.First(&tool, id).Error; err != nil {
		return nil, huma.NewError(http.StatusNotFound, "工具不存在")
	}
	return &tool, nil
}
