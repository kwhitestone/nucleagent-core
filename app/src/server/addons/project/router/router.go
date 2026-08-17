// Package project 的 HTTP 路由：Project CRUD。
//
// 项目按 user_id 隔离：列出/详情/更新/删除均校验归属（防 IDOR），不属于当前
// 用户的统一返回 404（不泄露存在性）。
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

// RegisterRoutes 注册 Project 路由。
func RegisterRoutes(api huma.API) {
	registerList(api)
	registerCreate(api)
	registerUpdate(api)
	registerDelete(api)
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

// ---- List ----

// ProjectListOutputBody 列表响应信封。
type ProjectListOutputBody struct {
	Code    int              `json:"code" example:"0"`
	Message string           `json:"message" example:"success"`
	Data    []*model.Project `json:"data"`
}

type listOutput struct {
	Body ProjectListOutputBody
}

func registerList(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "projectList",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/project",
		Summary:     "列出项目",
		Description: "返回当前用户的项目（按 user_id 过滤）",
		Tags:        []string{"Project"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, _ *struct{}) (*listOutput, error) {
		userID := userIDFromCtx(ctx)
		if userID == 0 {
			return nil, huma.NewError(http.StatusUnauthorized, "未认证")
		}
		var projects []*model.Project
		global.PRISM_DB.Where("user_id = ?", userID).Order("id DESC").Find(&projects)
		out := &listOutput{}
		out.Body.Code = 0
		out.Body.Message = "success"
		out.Body.Data = projects
		return out, nil
	})
}

// ---- Create ----

type projectCreateBody struct {
	Name string `json:"name" maxLength:"128" doc:"项目名称"`
}

type createInput struct {
	Body projectCreateBody
}

// ProjectCreateOutputBody 创建响应信封。
type ProjectCreateOutputBody struct {
	Code    int            `json:"code" example:"0"`
	Message string         `json:"message" example:"created"`
	Data    *model.Project `json:"data"`
}

type createOutput struct {
	Body ProjectCreateOutputBody
}

func registerCreate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "projectCreate",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/project",
		Summary:     "创建项目",
		Tags:        []string{"Project"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *createInput) (*createOutput, error) {
		userID := userIDFromCtx(ctx)
		if userID == 0 {
			return nil, huma.NewError(http.StatusUnauthorized, "未认证")
		}
		if input.Body.Name == "" {
			return nil, huma.NewError(http.StatusBadRequest, "name 不能为空")
		}
		project := &model.Project{
			UserID: userID,
			Name:   input.Body.Name,
		}
		if err := global.PRISM_DB.Create(project).Error; err != nil {
			global.PRISM_LOG.Error("project: 创建失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "创建失败")
		}
		out := &createOutput{}
		out.Body.Code = 0
		out.Body.Message = "created"
		out.Body.Data = project
		return out, nil
	})
}

// ---- Update ----

type projectPatchBody struct {
	Name       *string `json:"name,omitempty" maxLength:"128"`
	IsArchived *bool   `json:"isArchived,omitempty"`
}

type updateInput struct {
	ID   string `path:"id"`
	Body projectPatchBody
}

// ProjectUpdateOutputBody 更新响应信封。
type ProjectUpdateOutputBody struct {
	Code    int            `json:"code" example:"0"`
	Message string         `json:"message" example:"updated"`
	Data    *model.Project `json:"data"`
}

type updateOutput struct {
	Body ProjectUpdateOutputBody
}

func registerUpdate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "projectUpdate",
		Method:      http.MethodPatch,
		Path:        "/api/v1/addons/project/{id}",
		Summary:     "更新项目",
		Tags:        []string{"Project"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *updateInput) (*updateOutput, error) {
		project, err := loadOwnedProject(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{}
		if input.Body.Name != nil {
			updates["name"] = *input.Body.Name
		}
		if input.Body.IsArchived != nil {
			updates["is_archived"] = *input.Body.IsArchived
		}
		if len(updates) > 0 {
			if err := global.PRISM_DB.Model(&model.Project{}).Where("id = ?", project.ID).Updates(updates).Error; err != nil {
				global.PRISM_LOG.Error("project: 更新失败", zap.Error(err))
				return nil, huma.NewError(http.StatusInternalServerError, "更新失败")
			}
			global.PRISM_DB.First(project, project.ID)
		}
		out := &updateOutput{}
		out.Body.Code = 0
		out.Body.Message = "updated"
		out.Body.Data = project
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
		OperationID: "projectDelete",
		Method:      http.MethodDelete,
		Path:        "/api/v1/addons/project/{id}",
		Summary:     "删除项目",
		Tags:        []string{"Project"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *deleteInput) (*deleteOutput, error) {
		project, err := loadOwnedProject(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		if err := global.PRISM_DB.Delete(project).Error; err != nil {
			global.PRISM_LOG.Error("project: 删除失败", zap.Error(err))
			return nil, huma.NewError(http.StatusInternalServerError, "删除失败")
		}
		out := &deleteOutput{}
		out.Body.Code = 0
		out.Body.Message = "deleted"
		return out, nil
	})
}

// loadOwnedProject 按 id 加载项目，并校验属于当前用户（防 IDOR）。
// 不属于当前用户时返回 404（不泄露存在性）。
func loadOwnedProject(ctx context.Context, idStr string) (*model.Project, error) {
	userID := userIDFromCtx(ctx)
	if userID == 0 {
		return nil, huma.NewError(http.StatusUnauthorized, "未认证")
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
	}
	var project model.Project
	if err := global.PRISM_DB.First(&project, id).Error; err != nil {
		return nil, huma.NewError(http.StatusNotFound, "项目不存在")
	}
	if project.UserID != userID {
		return nil, huma.NewError(http.StatusNotFound, "项目不存在")
	}
	return &project, nil
}
