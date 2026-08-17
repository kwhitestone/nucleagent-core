// Package agent 的 HTTP 路由：Agent 模板列表（只读）。
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

// RegisterRoutes 注册 Agent 路由（模板只读 + 实例 CRUD）。
func RegisterRoutes(api huma.API) {
	registerListTemplates(api)
	registerHireInstance(api)
	registerListInstances(api)
	registerGetInstance(api)
	registerUpdateInstance(api)
	registerFireInstance(api)
}

// ctxKey 是 user_id 在 request context 中的键类型（避免字符串键冲突）。
type ctxKey int

const userIDKey ctxKey = 1

// UserIDKey 返回 user_id 的 context key（供 BridgeMiddleware 写入）。
func UserIDKey() ctxKey { return userIDKey }

// userIDFromCtx 从 request context 提取 user_id（由 BridgeMiddleware 写入）。
func userIDFromCtx(ctx context.Context) uint {
	v, _ := ctx.Value(userIDKey).(uint)
	return v
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

// ===== Agent Instances CRUD =====

// ---- Hire (Create) ----

// hireBody 雇佣 Agent 请求体：基于模板创建实例，可选 nickname/override。
// config/i18n 用 map[string]any：huma 把 model.JSON（json.RawMessage）生成为 string
// schema 会拒绝 JSON 对象输入（422）。map 才能正确生成 object schema。
type hireBody struct {
	TemplateID uint           `json:"templateId" doc:"来源模板ID"`
	Config     map[string]any `json:"config,omitempty" doc:"实例配置(nickname/override)"`
	I18n       map[string]any `json:"i18n,omitempty" doc:"多语言覆盖"`
}

type hireInput struct {
	Body hireBody
}

// AgentHireOutputBody 雇佣响应信封。
type AgentHireOutputBody struct {
	Code    int                  `json:"code" example:"0"`
	Message string               `json:"message" example:"created"`
	Data    *model.AgentInstance `json:"data"`
}

type hireOutput struct {
	Body AgentHireOutputBody
}

func registerHireInstance(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "agentHireInstance",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/agent/instances",
		Summary:     "雇佣 Agent（从模板创建实例）",
		Tags:        []string{"Agent"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *hireInput) (*hireOutput, error) {
		userID := userIDFromCtx(ctx)
		if userID == 0 {
			return nil, huma.NewError(http.StatusUnauthorized, "未认证")
		}
		if input.Body.TemplateID == 0 {
			return nil, huma.NewError(http.StatusBadRequest, "templateId 不能为空")
		}
		// 校验模板存在且启用。
		var tpl model.AgentTemplate
		if err := global.PRISM_DB.Where("id = ? AND is_active = ?", input.Body.TemplateID, true).
			First(&tpl).Error; err != nil {
			return nil, huma.NewError(http.StatusBadRequest, "模板不存在或未启用")
		}
		inst := &model.AgentInstance{
			UserID:     userID,
			TemplateID: input.Body.TemplateID,
			Config:     model.MustNewJSON(input.Body.Config),
			I18n:       model.MustNewJSON(input.Body.I18n),
		}
		if err := global.PRISM_DB.Create(inst).Error; err != nil {
			global.PRISM_LOG.Error("agent: 雇佣失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "雇佣失败")
		}
		out := &hireOutput{}
		out.Body.Code = 0
		out.Body.Message = "created"
		out.Body.Data = inst
		return out, nil
	})
}

// ---- List ----

// AgentInstancesOutputBody 实例列表响应信封。
type AgentInstancesOutputBody struct {
	Code    int                    `json:"code" example:"0"`
	Message string                 `json:"message" example:"success"`
	Data    []*model.AgentInstance `json:"data"`
}

type instancesOutput struct {
	Body AgentInstancesOutputBody
}

func registerListInstances(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "agentListInstances",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/agent/instances",
		Summary:     "列出已雇佣 Agent",
		Description: "返回当前用户雇佣的 Agent 实例（按 user_id 过滤）",
		Tags:        []string{"Agent"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, _ *struct{}) (*instancesOutput, error) {
		userID := userIDFromCtx(ctx)
		if userID == 0 {
			return nil, huma.NewError(http.StatusUnauthorized, "未认证")
		}
		var instances []*model.AgentInstance
		global.PRISM_DB.Where("user_id = ?", userID).Order("id DESC").Find(&instances)
		out := &instancesOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = instances
		return out, nil
	})
}

// ---- Get ----

// AgentInstanceOutputBody 实例详情响应信封。
type AgentInstanceOutputBody struct {
	Code    int                  `json:"code" example:"0"`
	Message string               `json:"message" example:"success"`
	Data    *model.AgentInstance `json:"data"`
}

type instanceOutput struct {
	Body AgentInstanceOutputBody
}

func registerGetInstance(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "agentGetInstance",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/agent/instances/{id}",
		Summary:     "Agent 实例详情",
		Tags:        []string{"Agent"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*instanceOutput, error) {
		inst, err := loadOwnedInstance(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		out := &instanceOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = inst
		return out, nil
	})
}

// ---- Update ----

// instancePatchBody 更新请求体（nickname/override 在 config 里；i18n 覆盖）。
type instancePatchBody struct {
	Config *map[string]any `json:"config,omitempty"`
	I18n   *map[string]any `json:"i18n,omitempty"`
}

type instanceUpdateInput struct {
	ID   string `path:"id"`
	Body instancePatchBody
}

type instanceUpdateOutput struct {
	Body AgentInstanceOutputBody
}

func registerUpdateInstance(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "agentUpdateInstance",
		Method:      http.MethodPatch,
		Path:        "/api/v1/addons/agent/instances/{id}",
		Summary:     "更新 Agent 实例（nickname/override）",
		Tags:        []string{"Agent"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *instanceUpdateInput) (*instanceUpdateOutput, error) {
		inst, err := loadOwnedInstance(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{}
		if input.Body.Config != nil {
			updates["config"] = model.MustNewJSON(*input.Body.Config)
		}
		if input.Body.I18n != nil {
			updates["i18n"] = model.MustNewJSON(*input.Body.I18n)
		}
		if len(updates) > 0 {
			if err := global.PRISM_DB.Model(&model.AgentInstance{}).Where("id = ?", inst.ID).Updates(updates).Error; err != nil {
				global.PRISM_LOG.Error("agent: 更新实例失败", zap.Error(err))
				return nil, huma.NewError(http.StatusInternalServerError, "更新失败")
			}
			global.PRISM_DB.First(inst, inst.ID)
		}
		out := &instanceUpdateOutput{}
		out.Body.Code = 0
		out.Body.Message = "updated"
		out.Body.Data = inst
		return out, nil
	})
}

// ---- Fire (Delete) ----

func registerFireInstance(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "agentFireInstance",
		Method:      http.MethodDelete,
		Path:        "/api/v1/addons/agent/instances/{id}",
		Summary:     "解雇 Agent",
		Tags:        []string{"Agent"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*struct {
		Body struct {
			Code    int    `json:"code" example:"0"`
			Message string `json:"message" example:"deleted"`
		}
	}, error) {
		inst, err := loadOwnedInstance(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		if err := global.PRISM_DB.Delete(inst).Error; err != nil {
			global.PRISM_LOG.Error("agent: 解雇失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "解雇失败")
		}
		return &struct {
			Body struct {
				Code    int    `json:"code" example:"0"`
				Message string `json:"message" example:"deleted"`
			}
		}{Body: struct {
			Code    int    `json:"code" example:"0"`
			Message string `json:"message" example:"deleted"`
		}{Code: 0, Message: "deleted"}}, nil
	})
}

// loadOwnedInstance 按 id 加载实例，并校验属于当前用户（防 IDOR）。
// 不属于当前用户时返回 404（不泄露存在性）。
func loadOwnedInstance(ctx context.Context, idStr string) (*model.AgentInstance, error) {
	userID := userIDFromCtx(ctx)
	if userID == 0 {
		return nil, huma.NewError(http.StatusUnauthorized, "未认证")
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
	}
	var inst model.AgentInstance
	if err := global.PRISM_DB.First(&inst, id).Error; err != nil {
		return nil, huma.NewError(http.StatusNotFound, "Agent 实例不存在")
	}
	if inst.UserID != userID {
		return nil, huma.NewError(http.StatusNotFound, "Agent 实例不存在")
	}
	return &inst, nil
}
