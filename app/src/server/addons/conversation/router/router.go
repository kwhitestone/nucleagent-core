package router

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

// ConversationItem 对话列表项（骨架占位）。
type ConversationItem struct {
	ID    uint   `json:"id" doc:"对话ID"`
	Title string `json:"title" doc:"对话标题"`
}

// ConversationListOutput 对话列表响应。
type ConversationListOutput struct {
	Body struct {
		Code int                `json:"code" example:"0" doc:"状态码"`
		Data []ConversationItem `json:"data" doc:"对话列表"`
	}
}

// RegisterRoutes 注册 Conversation 路由到 Huma。
// 骨架阶段：仅占位，Conversation/Message/Step 的 CRUD + SSE 流待实现。
func RegisterRoutes(api huma.API) {
	// TODO: Conversation/Message/Step CRUD + SSE 流（SSE 经 Redis pub/sub 支持多实例）
	huma.Register(api, huma.Operation{
		OperationID: "conversationList",
		Method:      "GET",
		Path:        "/api/v1/addons/conversation/conversations",
		Summary:     "获取对话列表",
		Description: "骨架占位接口，CRUD 待实现",
		Tags:        []string{"Conversation"},
	}, func(ctx context.Context, input *struct{}) (*ConversationListOutput, error) {
		resp := &ConversationListOutput{}
		resp.Body.Code = 0
		resp.Body.Data = []ConversationItem{} // 骨架占位
		return resp, nil
	})
}
