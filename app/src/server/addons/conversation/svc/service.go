// Package svc 对话编排服务：创建对话 -> 签发 TempLLMKey -> 调度到 executor -> 收回报。
//
// 实现 executorreg.Handler，处理 executor 上报的 stream_event / task_result / heartbeat。
package svc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nucleagent/nucleagent-shared/a2a"
	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/nucleagent/nucleagent-shared/model"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"nucleagent-core/addons/conversation/stream"
	"nucleagent-core/addons/executorreg"
	"nucleagent-core/addons/llmproxy"

	"whitestone.top/prism-fusion/global"
	"gorm.io/gorm"
)

// Service 对话编排服务。
type Service struct {
	mu          sync.Mutex
	running     map[uint]*runState // conversationID -> 运行态
	tempKeyTTL  time.Duration
	leaseTimeout time.Duration // executor 租约超时（超时标记 failed，防 goroutine 泄露）
}

// runState 单次执行运行态（core 侧）。
type runState struct {
	StepID        string
	DelegationID  string
	SenderSlug    string // agent 展示名（用于流式 upsert 幂等键）
	ExecBackend   string
	ProviderID    uint
	Model         string
	CancelFn      context.CancelFunc
}

// runStateKey 把 conversationID 映射到运行态（按 delegation 隔离）。
// v1 每个 conversation 同时只有一个活跃 delegation。

// NewService 构造服务。
func NewService() *Service {
	return &Service{
		running:      make(map[uint]*runState),
		tempKeyTTL:   30 * time.Minute,
		leaseTimeout: 10 * time.Minute, // executor 无回报超时（v1 固定值；可配）
	}
}

// Default 全局服务实例。
var Default = NewService()

// CreateAndExecute 创建对话并调度执行。
//
// 1. 写 conversations + user message
// 2. 签 TempLLMKey
// 3. 选 executor，下发 a2a_request
// 4. 创建 streaming 占位（由首个 text_delta 触发）
func (s *Service) CreateAndExecute(ctx context.Context, userID uint, req *CreateRequest) (*model.Conversation, error) {
	db := global.PRISM_DB

	// 1. 创建 conversation。
	conv := &model.Conversation{
		UserID:     userID,
		Title:      firstLine(req.Input, 80),
		Mode:       req.Mode,
		Status:     "executing",
		ProviderID: req.ProviderID,
		Model:      req.Model,
		AgentID:    req.AgentID,
		ProjectID:  req.ProjectID,
	}
	if conv.Mode == "" {
		conv.Mode = "a2a"
	}
	if err := db.Create(conv).Error; err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}

	// 2. 写 user message。
	userMsg := &model.Message{
		ConversationID: conv.ID,
		SenderType:     model.SenderTypeUser,
		SenderName:     "user",
		MsgType:        model.MsgTypeText,
		Content:        req.Input,
	}
	if err := db.Create(userMsg).Error; err != nil {
		return nil, err
	}
	stream.Default.PublishCreated(conv.ID, userMsg)

	// 3. 异步调度执行（不阻塞 HTTP 响应）。
	go s.dispatch(context.Background(), conv, req.Input)

	return conv, nil
}

// dispatch 签发 TempLLMKey + 选 executor + 下发 a2a_request。
func (s *Service) dispatch(ctx context.Context, conv *model.Conversation, input string) {
	stepID := uuid.NewString()
	delegationID := uuid.NewString()
	senderSlug := "agent"

	// 签 TempLLMKey。
	var providerID uint
	if conv.ProviderID != nil {
		providerID = *conv.ProviderID
	}
	tempKey := llmproxy.Default.Issue(conv.ID, conv.UserID, providerID, conv.Model, s.tempKeyTTL)

	// 记录运行态。runCtx 带租约超时，避免 executor 永不回报时 goroutine 泄露。
	runCtx, cancel := context.WithTimeout(ctx, s.leaseTimeout)
	rs := &runState{
		StepID:       stepID,
		DelegationID: delegationID,
		SenderSlug:   senderSlug,
		ExecBackend:  reqBackendForMode(conv.Mode),
		ProviderID:   providerID,
		Model:        conv.Model,
		CancelFn:     cancel,
	}
	s.mu.Lock()
	// 若已有旧 runState（如重复调度），先取消旧的，避免 goroutine 泄露。
	if old, exists := s.running[conv.ID]; exists && old.CancelFn != nil {
		old.CancelFn()
	}
	s.running[conv.ID] = rs
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		// 仅当当前 runState 仍是自己时才删除（避免删掉新调度的）。
		if cur, ok := s.running[conv.ID]; ok && cur == rs {
			delete(s.running, conv.ID)
		}
		s.mu.Unlock()
	}()

	// 选 executor 连接。
	conn, ok := executorreg.Default.Pick("")
	if !ok {
		s.failConversation(conv, "no executor available")
		return
	}

	// 构造 a2a_request。
	execReq := a2a.ExecutionRequest{
		ConversationID: conv.ID,
		StepID:         stepID,
		Mode:           conv.Mode,
		ProviderID:     conv.ProviderID,
		Model:          conv.Model,
		Input:          input,
		Headers: map[string]string{
			llm.KeyHeader: tempKey.Key,
		},
	}
	body, _ := json.Marshal(execReq)
	reqEnv, _ := a2a.NewEnvelopeWithRequest(time.Now().UnixMilli(), a2a.EnvA2ARequest, delegationID, a2a.A2ARequestPayload{
		Method:     "message/send",
		Capability: rs.ExecBackend,
		Headers:    map[string]string{llm.KeyHeader: tempKey.Key},
		Body:       body,
		Stream:     true,
	})

	// 下发。executor 异步执行，结果通过 OnTaskResult 回报。
	if err := conn.Send(reqEnv); err != nil {
		s.failConversation(conv, "dispatch failed: "+err.Error())
		llmproxy.Default.RevokeByConversation(conv.ID)
		return
	}

	// 等待 ctx 取消（kill）或结果回报（OnTaskResult 删除 running + cancel）。
	<-runCtx.Done()

	// 区分：租约超时（executor 无回报）需标记失败；被 cancel（kill/结果到达）则不处理。
	if runCtx.Err() == context.DeadlineExceeded {
		s.mu.Lock()
		// 确认仍未被 OnTaskResult 处理。
		_, stillRunning := s.running[conv.ID]
		if stillRunning {
			delete(s.running, conv.ID)
		}
		s.mu.Unlock()
		if stillRunning {
			s.failConversation(conv, "executor lease timeout (no result)")
			llmproxy.Default.RevokeByConversation(conv.ID)
		}
	}
}

// failConversation 标记对话失败 + 写错误消息。
func (s *Service) failConversation(conv *model.Conversation, reason string) {
	db := global.PRISM_DB
	db.Model(&model.Conversation{}).Where("id = ?", conv.ID).
		Update("status", "failed")
	errMsg := &model.Message{
		ConversationID: conv.ID,
		SenderType:     model.SenderTypeSystem,
		SenderName:     "system",
		MsgType:        model.MsgTypeError,
		Content:        "❌ " + reason,
	}
	db.Create(errMsg)
	stream.Default.PublishCreated(conv.ID, errMsg)
}

// --- executorreg.Handler 实现 ---

// OnStreamEvent 处理 executor 上报的流式事件。
func (s *Service) OnStreamEvent(env *a2a.Envelope, p a2a.A2AStreamEventPayload) {
	db := global.PRISM_DB
	s.mu.Lock()
	rs, ok := s.running[p.ConversationID]
	s.mu.Unlock()
	if !ok {
		return
	}

	switch p.EventType {
	case "text_delta":
		// 累积式：取出当前 content 追加。v1 简化：每次 upsert 用「已累积」内容。
		// 真实实现需在 rs 里维护 accumulator；这里用 append 到已有 streaming 行。
		appendToStreaming(db, p.ConversationID, rs.StepID, rs.DelegationID, rs.SenderSlug, p.Content)
	case "thinking_delta":
		// thinking 单独存一条 streaming 行（senderSlug 加 .thinking 后缀做幂等隔离）。
		appendToStreaming(db, p.ConversationID, rs.StepID, rs.DelegationID, rs.SenderSlug+".thinking", p.Content)
	case "progress":
		// progress 不持久化到 message（太碎），仅推送 status 事件给前端。
		stream.Default.PublishStatus(p.ConversationID, &model.Message{
			ConversationID: p.ConversationID,
			SenderType:     model.SenderTypeSystem,
			SenderName:     "system",
			MsgType:        model.MsgTypeStatus,
			Content:        p.Content,
		})
	case "tool_use":
		// tool_use 记一条 tool_call 消息。
		toolMsg := &model.Message{
			ConversationID: p.ConversationID,
			SenderType:     model.SenderTypeTool,
			SenderName:     p.Tool,
			MsgType:        model.MsgTypeToolCall,
			Content:        p.Content,
			Metadata:       model.MustNewJSON(map[string]any{"step_id": rs.StepID, "tool": p.Tool}),
		}
		db.Create(toolMsg)
		stream.Default.PublishCreated(p.ConversationID, toolMsg)
	}
}

// OnTaskResult 处理 executor 上报的最终结果。
//
// 幂等：删除 running 条目，若不存在说明已处理（重复/乱序结果直接丢弃，不重复 finalize）。
func (s *Service) OnTaskResult(env *a2a.Envelope, p a2a.A2ATaskResultPayload) {
	db := global.PRISM_DB
	s.mu.Lock()
	rs, ok := s.running[p.ConversationID]
	if !ok {
		// 已处理过（重复/乱序结果），幂等丢弃。
		s.mu.Unlock()
		global.PRISM_LOG.Info("OnTaskResult: conversation not in running (already handled/duplicate)",
			zap.Uint("convID", p.ConversationID), zap.String("status", p.Status))
		return
	}
	// 删除 running 条目，确保重复结果幂等。
	delete(s.running, p.ConversationID)
	if rs.CancelFn != nil {
		rs.CancelFn() // 取消 dispatch 的 runCtx（让 dispatch goroutine 退出）。
	}
	s.mu.Unlock()

	// 解析结果体。
	var result a2a.ExecutionResult
	if len(p.Body) > 0 {
		_ = json.Unmarshal(p.Body, &result)
	}

	// 提升 streaming 行为最终 result 消息。
	finalType := model.MsgTypeResult
	if p.Status == "failed" || p.Status == "killed" {
		finalType = model.MsgTypeError
	}
	content := result.Output
	if content == "" {
		content = result.Error
	}
	if err := finalizeStreaming(db, stream.Default, p.ConversationID, rs.StepID, rs.DelegationID, rs.SenderSlug, finalType, content); err != nil {
		global.PRISM_LOG.Warn("finalize streaming failed", zap.Error(err))
	}

	// 更新 conversation 状态。
	status := "completed"
	if p.Status == "failed" {
		status = "failed"
	} else if p.Status == "killed" {
		status = "cancelled"
	}
	now := time.Now()
	db.Model(&model.Conversation{}).Where("id = ?", p.ConversationID).
		Updates(map[string]any{"status": status, "completed_at": now})

	// 撤销 TempLLMKey。
	llmproxy.Default.RevokeByConversation(p.ConversationID)
}

// OnHeartbeat 处理 executor 心跳：更新 heartbeat_at / lease_until。
func (s *Service) OnHeartbeat(env *a2a.Envelope, p a2a.A2AHeartbeatBatchPayload) {
	db := global.PRISM_DB
	now := time.Now()
	leaseUntil := now.Add(45 * time.Second)
	for _, item := range p.Items {
		db.Model(&model.Conversation{}).Where("id = ?", item.ConversationID).
			Updates(map[string]any{"heartbeat_at": now, "lease_until": leaseUntil})
	}
}

// Cancel 取消运行中的对话（前端 POST /cancel 调用）。
func (s *Service) Cancel(convID uint) error {
	s.mu.Lock()
	rs, ok := s.running[convID]
	s.mu.Unlock()
	if !ok {
		return gorm.ErrRecordNotFound
	}
	// 下发 task_kill。
	conn, ok := executorreg.Default.Pick("")
	if ok {
		killEnv, _ := a2a.NewEnvelopeNow(a2a.EnvTaskKill, a2a.TaskKillPayload{ConversationIDs: []uint{convID}})
		_ = conn.Send(killEnv)
	}
	if rs.CancelFn != nil {
		rs.CancelFn()
	}
	return nil
}

// appendToStreaming 追加 delta 到 streaming 行（累积 content）。
func appendToStreaming(db *gorm.DB, convID uint, stepID, delegationID, senderSlug, delta string) {
	if delta == "" {
		return
	}
	// 取当前累积 content。
	key := streamKey(convID, senderSlug, delegationID, stepID)
	mu := streamLock(key)
	mu.Lock()
	defer mu.Unlock()

	var existing model.Message
	sub, subArg := streamKeyClause(db, key)
	err := db.Where("conversation_id = ? AND msg_type = ? AND "+sub,
		convID, model.MsgTypeStreaming, subArg).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		// 创建 + 放入 delta。
		msg := &model.Message{
			ConversationID: convID,
			SenderType:     model.SenderTypeAgent,
			SenderName:     senderSlug,
			MsgType:        model.MsgTypeStreaming,
			Content:        delta,
			Metadata: model.MustNewJSON(map[string]any{
				"step_id":       stepID,
				"delegation_id": delegationID,
				"stream_key":    key,
			}),
		}
		if err := db.Create(msg).Error; err == nil {
			stream.Default.PublishCreated(convID, msg)
		}
		return
	}
	if err != nil {
		return
	}
	// 追加。
	existing.Content += delta
	db.Model(&model.Message{}).Where("id = ?", existing.ID).Update("content", existing.Content)
	stream.Default.PublishUpdated(convID, &existing)
}

// reqBackendForMode 按 mode 选执行后端（v1 全用 mock-llm，真实路由后置）。
func reqBackendForMode(mode string) string {
	return "mock-llm"
}

// firstLine 取输入首行，截断到 maxLen。
func firstLine(s string, maxLen int) string {
	for i, r := range s {
		if r == '\n' {
			s = s[:i]
			break
		}
	}
	if len([]rune(s)) > maxLen {
		r := []rune(s)
		s = string(r[:maxLen]) + "…"
	}
	return s
}

// CreateRequest 创建对话请求。
type CreateRequest struct {
	Mode       string `json:"mode" required:"true"`
	Input      string `json:"input" required:"true"`
	ProviderID *uint  `json:"providerId,omitempty"`
	Model      string `json:"model,omitempty"`
	AgentID    *uint  `json:"agentId,omitempty"`
	ProjectID  *uint  `json:"projectId,omitempty"`
}

// 确保实现 executorreg.Handler。
var _ executorreg.Handler = (*Service)(nil)
