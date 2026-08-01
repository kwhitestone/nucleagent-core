// Package conversation 的 HTTP 路由：Conversation CRUD + SSE 流 + 消息/步骤/日志查询。
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"

	"nucleagent-core/addons/conversation/svc"
	"nucleagent-core/addons/conversation/stream"

	"whitestone.top/prism-fusion/global"
)

// RegisterRoutes 注册 Conversation 路由到 Huma。
func RegisterRoutes(api huma.API) {
	registerCreate(api)
	registerList(api)
	registerGet(api)
	registerDelete(api)
	registerMessages(api)
	registerStream(api) // SSE
	registerCancel(api)
}

// ---- Create ----

// CreateInput 创建对话请求。
type CreateInput struct {
	Body svc.CreateRequest
}

// CreateOutput 创建对话响应。
type CreateOutput struct {
	Body struct {
		Code    int                `json:"code" example:"0"`
		Message string             `json:"message" example:"success"`
		Data    *model.Conversation `json:"data"`
	}
}

func registerCreate(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationCreate",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/conversation",
		Summary:     "创建对话",
		Description: "创建对话并调度执行（异步）",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *CreateInput) (*CreateOutput, error) {
		userID := userIDFromCtx(ctx)
		if userID == 0 {
			return nil, huma.NewError(http.StatusUnauthorized, "未认证")
		}
		if input.Body.Input == "" {
			return nil, huma.NewError(http.StatusBadRequest, "input 不能为空")
		}
		conv, err := svc.Default.CreateAndExecute(ctx, userID, &input.Body)
		if err != nil {
			return nil, huma.NewError(http.StatusInternalServerError, err.Error())
		}
		resp := &CreateOutput{}
		resp.Body.Code = 0
		resp.Body.Message = "created"
		resp.Body.Data = conv
		return resp, nil
	})
}

// ---- List ----

// ListOutput 对话列表响应。
type ListOutput struct {
	Body struct {
		Code int                  `json:"code" example:"0"`
		Data []model.Conversation `json:"data"`
	}
}

func registerList(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationList",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/conversation",
		Summary:     "列出对话",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct{}) (*ListOutput, error) {
		userID := userIDFromCtx(ctx)
		if userID == 0 {
			return nil, huma.NewError(http.StatusUnauthorized, "未认证")
		}
		var convs []model.Conversation
		global.PRISM_DB.Where("user_id = ?", userID).Order("id DESC").Limit(100).Find(&convs)
		resp := &ListOutput{}
		resp.Body.Code = 0
		resp.Body.Data = convs
		return resp, nil
	})
}

// ---- Get ----

// GetOutput 对话详情响应。
type GetOutput struct {
	Body struct {
		Code int                `json:"code" example:"0"`
		Data *model.Conversation `json:"data"`
	}
}

func registerGet(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationGet",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/conversation/{id}",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*GetOutput, error) {
		conv, err := loadOwnedConversation(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		resp := &GetOutput{}
		resp.Body.Code = 0
		resp.Body.Data = conv
		return resp, nil
	})
}

// loadOwnedConversation 按 id 加载对话，并校验属于当前用户（防 IDOR）。
// 不属于当前用户时返回 404（不泄露存在性）。
func loadOwnedConversation(ctx context.Context, idStr string) (*model.Conversation, error) {
	userID := userIDFromCtx(ctx)
	if userID == 0 {
		return nil, huma.NewError(http.StatusUnauthorized, "未认证")
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, huma.NewError(http.StatusBadRequest, "无效 ID")
	}
	var conv model.Conversation
	if err := global.PRISM_DB.First(&conv, id).Error; err != nil {
		return nil, huma.NewError(http.StatusNotFound, "对话不存在")
	}
	if conv.UserID != userID {
		// 不泄露存在性，统一返回 404。
		return nil, huma.NewError(http.StatusNotFound, "对话不存在")
	}
	return &conv, nil
}

// ---- Delete ----

func registerDelete(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationDelete",
		Method:      http.MethodDelete,
		Path:        "/api/v1/addons/conversation/{id}",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*struct{ Body struct{ Code int } `json:"body"` }, error) {
		// 先校验归属（防 IDOR 删除他人对话）。
		if _, err := loadOwnedConversation(ctx, input.ID); err != nil {
			return nil, err
		}
		id, _ := strconv.ParseUint(input.ID, 10, 64)
		global.PRISM_DB.Where("conversation_id = ?", id).Delete(&model.Message{})
		global.PRISM_DB.Delete(&model.Conversation{}, id)
		return &struct{ Body struct{ Code int } `json:"body"` }{Body: struct{ Code int }{Code: 0}}, nil
	})
}

// ---- Messages ----

// MessagesOutput 消息列表响应。
type MessagesOutput struct {
	Body struct {
		Code int             `json:"code" example:"0"`
		Data []model.Message `json:"data"`
	}
}

func registerMessages(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationMessages",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/conversation/{id}/messages",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID      string `path:"id"`
		AfterID uint   `query:"afterId" doc:"返回 id 大于此值的消息（分页/补齐）"`
	}) (*MessagesOutput, error) {
		conv, err := loadOwnedConversation(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		var msgs []model.Message
		q := global.PRISM_DB.Where("conversation_id = ?", conv.ID)
		if input.AfterID > 0 {
			q = q.Where("id > ?", input.AfterID)
		}
		q.Order("id ASC").Limit(500).Find(&msgs)
		resp := &MessagesOutput{}
		resp.Body.Code = 0
		resp.Body.Data = msgs
		return resp, nil
	})
}

// ---- SSE Stream ----

func registerStream(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationStream",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/conversation/{id}/messages/stream",
		Summary:     "SSE 流式订阅对话消息",
		Description: "附录 §7.3：先订阅 broker，再读 backlog，避免 gap；支持 Last-Event-ID 重连",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID          string `path:"id"`
		LastEventID string `header:"Last-Event-ID"`
	}) (*huma.StreamResponse, error) {
		// 校验归属（防 IDOR 订阅他人对话的 SSE 流）。
		conv, err := loadOwnedConversation(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		convID := conv.ID
		afterID := parseAfterID(input.LastEventID)

		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				serveSSE(hctx, convID, afterID)
			},
		}, nil
	})
}

// ---- Cancel ----

func registerCancel(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "conversationCancel",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/conversation/{id}/cancel",
		Tags:        []string{"Conversation"},
		Security:    []map[string][]string{{"AuthTokenAuth": {}}},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*struct{ Body struct{ Code int } `json:"body"` }, error) {
		// 先校验归属（防 IDOR 取消他人执行）。
		conv, err := loadOwnedConversation(ctx, input.ID)
		if err != nil {
			return nil, err
		}
		if err := svc.Default.Cancel(conv.ID); err != nil {
			return nil, huma.NewError(http.StatusNotFound, "对话未在执行")
		}
		return &struct{ Body struct{ Code int } `json:"body"` }{Body: struct{ Code int }{Code: 0}}, nil
	})
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

// parseAfterID 解析 Last-Event-ID（SSE event ID = message ID）。
func parseAfterID(s string) uint {
	if s == "" {
		return 0
	}
	id, _ := strconv.ParseUint(s, 10, 64)
	return uint(id)
}

// serveSSE 执行 SSE 推送：先订阅，再读 backlog，再 live loop。
func serveSSE(hctx huma.Context, convID, afterID uint) {
	hctx.SetHeader("Content-Type", "text/event-stream")
	hctx.SetHeader("Cache-Control", "no-cache")
	hctx.SetHeader("Connection", "keep-alive")
	hctx.SetHeader("X-Accel-Buffering", "no") // nginx 不缓冲

	// 1. 先订阅 broker（避免 backlog 读取期间的新消息丢失）。
	ch, unsub := stream.Default.Subscribe(convID)
	defer unsub()

	// 2. 读 backlog。
	var backlog []model.Message
	q := global.PRISM_DB.Where("conversation_id = ?", convID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	q.Order("id ASC").Limit(500).Find(&backlog)
	w := hctx.BodyWriter()
	flusher, _ := w.(http.Flusher)
	for i := range backlog {
		writeSSEEvent(w, flusher, "message-created", backlog[i].ID, backlog[i])
	}

	// 3. live loop。
	for ev := range ch {
		switch ev.Kind {
		case stream.KindCreated, stream.KindStatus:
			if ev.Message != nil {
				writeSSEEvent(w, flusher, "message-created", ev.Message.ID, ev.Message)
			}
		case stream.KindUpdated:
			if ev.Message != nil {
				writeSSEEvent(w, flusher, "message-updated", ev.Message.ID, ev.Message)
			}
		case stream.KindDeleted:
			writeSSEEvent(w, flusher, "message-deleted", ev.MessageID, map[string]uint{"id": ev.MessageID})
		}
	}
}

// writeSSEEvent 写一个 SSE 事件帧。
func writeSSEEvent(w interface{ Write([]byte) (int, error) }, flusher http.Flusher, event string, id uint, data any) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, event, string(payload))
	if flusher != nil {
		flusher.Flush()
	}
}

// 占位：避免 zap 未用 import 警告（后续日志扩展用）。
var _ = zap.NewNop
