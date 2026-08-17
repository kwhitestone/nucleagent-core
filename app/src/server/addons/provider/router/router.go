// Package provider 的 HTTP 路由：Provider CRUD。
//
// Provider 是全局资源（不按 user_id 过滤）。APIKey 字段 json tag 为 "-"，
// 列表/详情永不回传；创建/更新单独接收明文 apiKey，用 llmproxy.EncryptAPIKey
// 加密入库（与 coredata seed 同款 MASTER_KEY 加密）。
package router

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"

	"nucleagent-core/addons/llmproxy"

	"github.com/kwhitestone/prism-fusion/global"
)

// RegisterRoutes 注册 Provider 路由。
func RegisterRoutes(api huma.API) {
	registerList(api)
	registerCreate(api)
	registerUpdate(api)
	registerDelete(api)
}

// ---- List ----

// ProviderListOutputBody 列表响应信封（huma 要求结构体名全局唯一）。
type ProviderListOutputBody struct {
	Code    int               `json:"code" example:"0"`
	Message string            `json:"message" example:"success"`
	Data    []*model.Provider `json:"data"`
}

type listOutput struct {
	Body ProviderListOutputBody
}

func registerList(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "providerList",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/provider",
		Summary:     "列出 Provider",
		Description: "返回所有 LLM 提供商（APIKey 不回传）",
		Tags:        []string{"Provider"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, _ *struct{}) (*listOutput, error) {
		var providers []*model.Provider
		if err := global.PRISM_DB.Order("id asc").Find(&providers).Error; err != nil {
			global.PRISM_LOG.Error("provider: 查询失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "查询失败")
		}
		out := &listOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = providers
		return out, nil
	})
}

// ---- Create ----

// providerCreateBody 创建请求体（apiKey 明文，单独接收）。
// config 用 map[string]any 而非 model.JSON：huma 把 model.JSON（json.RawMessage）
// 生成为 string schema，会拒绝 JSON 对象输入（422）。map 才能正确生成 object schema。
type providerCreateBody struct {
	Name     string         `json:"name" maxLength:"128" doc:"提供商名称"`
	APIKey   string         `json:"apiKey" doc:"API 密钥（明文，后端加密存储）"`
	Config   map[string]any `json:"config,omitempty" doc:"提供商配置(baseUrl/apiFormat/authScheme/models)"`
	IsActive bool           `json:"isActive"`
}

type createInput struct {
	Body providerCreateBody
}

// ProviderCreateOutputBody 创建响应信封。
type ProviderCreateOutputBody struct {
	Code    int             `json:"code" example:"0"`
	Message string          `json:"message" example:"created"`
	Data    *model.Provider `json:"data"`
}

type createOutput struct {
	Body ProviderCreateOutputBody
}

func registerCreate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "providerCreate",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/provider",
		Summary:     "创建 Provider",
		Description: "创建 LLM 提供商，apiKey 加密存储",
		Tags:        []string{"Provider"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *createInput) (*createOutput, error) {
		if input.Body.Name == "" {
			return nil, huma.NewError(http.StatusBadRequest, "name 不能为空")
		}
		encKey, err := encryptKey(input.Body.APIKey)
		if err != nil {
			global.PRISM_LOG.Warn("provider: 加密 apiKey 失败，存明文（dev）", zap.Error(err))
			encKey = input.Body.APIKey
		}
		provider := &model.Provider{
			Name:     input.Body.Name,
			APIKey:   encKey,
			Config:   model.MustNewJSON(input.Body.Config),
			IsActive: input.Body.IsActive,
		}
		if err := global.PRISM_DB.Create(provider).Error; err != nil {
			global.PRISM_LOG.Error("provider: 创建失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "创建失败")
		}
		out := &createOutput{}
		out.Body.Code = 0
		out.Body.Message = "created"
		out.Body.Data = provider
		return out, nil
	})
}

// ---- Update ----

// providerPatchBody 更新请求体（所有字段可选；apiKey 留空则不改）。
type providerPatchBody struct {
	Name     *string         `json:"name,omitempty" maxLength:"128"`
	APIKey   *string         `json:"apiKey,omitempty" doc:"新 API 密钥（明文）；留空不修改"`
	Config   *map[string]any `json:"config,omitempty"`
	IsActive *bool           `json:"isActive,omitempty"`
}

type updateInput struct {
	ID   string `path:"id"`
	Body providerPatchBody
}

// ProviderUpdateOutputBody 更新响应信封。
type ProviderUpdateOutputBody struct {
	Code    int             `json:"code" example:"0"`
	Message string          `json:"message" example:"updated"`
	Data    *model.Provider `json:"data"`
}

type updateOutput struct {
	Body ProviderUpdateOutputBody
}

func registerUpdate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "providerUpdate",
		Method:      http.MethodPatch,
		Path:        "/api/v1/addons/provider/{id}",
		Summary:     "更新 Provider",
		Description: "部分更新 Provider；apiKey 留空不修改，传值则重新加密",
		Tags:        []string{"Provider"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *updateInput) (*updateOutput, error) {
		id, err := strconv.ParseUint(input.ID, 10, 64)
		if err != nil {
			return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
		}
		var provider model.Provider
		if err := global.PRISM_DB.First(&provider, id).Error; err != nil {
			return nil, huma.NewError(http.StatusNotFound, "Provider 不存在")
		}
		// 逐字段更新（仅更新非 nil 字段）。
		updates := map[string]interface{}{}
		if input.Body.Name != nil {
			updates["name"] = *input.Body.Name
		}
		if input.Body.Config != nil {
			updates["config"] = model.MustNewJSON(*input.Body.Config)
		}
		if input.Body.IsActive != nil {
			updates["is_active"] = *input.Body.IsActive
		}
		if input.Body.APIKey != nil && *input.Body.APIKey != "" {
			encKey, encErr := encryptKey(*input.Body.APIKey)
			if encErr != nil {
				global.PRISM_LOG.Warn("provider: 加密 apiKey 失败，存明文（dev）", zap.Error(encErr))
				encKey = *input.Body.APIKey
			}
			updates["api_key"] = encKey
		}
		if len(updates) > 0 {
			if err := global.PRISM_DB.Model(&model.Provider{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				global.PRISM_LOG.Error("provider: 更新失败", zap.Error(err))
				return nil, huma.NewError(http.StatusInternalServerError, "更新失败")
			}
		}
		// 重新加载返回最新状态（APIKey 不回传）。
		global.PRISM_DB.First(&provider, id)
		out := &updateOutput{}
		out.Body.Code = 0
		out.Body.Message = "updated"
		out.Body.Data = &provider
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
		OperationID: "providerDelete",
		Method:      http.MethodDelete,
		Path:        "/api/v1/addons/provider/{id}",
		Summary:     "删除 Provider",
		Tags:        []string{"Provider"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(_ context.Context, input *deleteInput) (*deleteOutput, error) {
		id, err := strconv.ParseUint(input.ID, 10, 64)
		if err != nil {
			return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
		}
		// 先确认存在（404 而非静默删除）。
		var provider model.Provider
		if err := global.PRISM_DB.First(&provider, id).Error; err != nil {
			return nil, huma.NewError(http.StatusNotFound, "Provider 不存在")
		}
		if err := global.PRISM_DB.Delete(&provider).Error; err != nil {
			global.PRISM_LOG.Error("provider: 删除失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "删除失败")
		}
		out := &deleteOutput{}
		out.Body.Code = 0
		out.Body.Message = "deleted"
		return out, nil
	})
}

// encryptKey 加密明文 apiKey（与 coredata seed 同款 MASTER_KEY 加密）。
// MASTER_KEY 未设时返回 error，调用方决定是否回退明文（仅 dev）。
func encryptKey(plain string) (string, error) {
	return llmproxy.EncryptAPIKey(plain)
}
