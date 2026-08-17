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

	"github.com/kwhitestone/prism-fusion/global"
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

	// 0. 先核对附件再落任何库记录 —— 附件不合法就整个请求失败，
	//    否则会留下一条引用了不存在文件的对话，用户以为传成功了。
	atts, err := resolveAttachments(ctx, req.Attachments)
	if err != nil {
		return nil, err
	}

	// 0.5 校验模型选择。同样在落库之前 —— 否则会留下一条指向不可用模型的对话，
	//     它能创建成功却在执行时才失败，用户看到的是「发出去了但报错」。
	var providerID uint
	if req.ProviderID != nil {
		providerID = *req.ProviderID
	}
	if err := ValidateSelection(providerID, req.Model); err != nil {
		return nil, err
	}

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

	// 2. 写 user message（附件清单进 metadata，供后续轮次重建历史时带上）。
	userMsg := &model.Message{
		ConversationID: conv.ID,
		SenderType:     model.SenderTypeUser,
		SenderName:     "user",
		MsgType:        model.MsgTypeText,
		Content:        req.Input,
		Metadata:       attachmentsToMetadata(atts),
	}
	if err := db.Create(userMsg).Error; err != nil {
		return nil, err
	}
	stream.Default.PublishCreated(conv.ID, userMsg)

	// 3. 异步调度执行（不阻塞 HTTP 响应）。
	//    新建对话 executor 侧本就没有 session，无需重置信号。
	go s.dispatch(context.Background(), conv, req.Input, atts, false)

	return conv, nil
}

// FollowUp 在已有对话上追加用户消息并重新调度执行（多轮对话）。
//
// 与 CreateAndExecute 的区别：不新建 conversation，只追加 user message，
// 然后复用 dispatch 重新下发 a2a_request。status 回到 executing。
// sel 非 nil 时表示本轮同时切换模型/provider：先落库再 dispatch，并要求
// executor 重建 hermes session（否则新模型不生效，见 dispatch 的 sessionReset）。
func (s *Service) FollowUp(ctx context.Context, conv *model.Conversation, input string, attachments []AttachmentInput, sel *ModelSelection) error {
	db := global.PRISM_DB

	// 0. 先核对附件（同 CreateAndExecute：不合法则整个追问失败，不留半成品消息）。
	atts, err := resolveAttachments(ctx, attachments)
	if err != nil {
		return err
	}

	// 0.5 模型切换：校验 + 落库。放在写 message 之前 —— 校验失败就整个追问失败，
	//     不留一条「用户消息已存但模型没换成」的错位状态。
	modelChanged, err := s.applyModelSelection(conv, sel)
	if err != nil {
		return err
	}

	// 1. 写 user message（附件清单进 metadata）。
	userMsg := &model.Message{
		ConversationID: conv.ID,
		SenderType:     model.SenderTypeUser,
		SenderName:     "user",
		MsgType:        model.MsgTypeText,
		Content:        input,
		Metadata:       attachmentsToMetadata(atts),
	}
	if err := db.Create(userMsg).Error; err != nil {
		return err
	}
	stream.Default.PublishCreated(conv.ID, userMsg)

	// 2. 状态回到 executing，清掉完成时间。
	db.Model(&model.Conversation{}).Where("id = ?", conv.ID).
		Updates(map[string]any{"status": "executing", "completed_at": nil})

	// 3. 异步调度执行（不阻塞 HTTP 响应）。
	//
	//    重置信号有两个来源，缺一不可：
	//      - modelChanged：本轮 follow-up 自带模型切换；
	//      - takePendingReset：先前通过 PATCH 切过模型（那时库里已是新值，
	//        本轮比对看不出差异，只能靠持久化标记）。
	//    没变时不重建，否则白扔 hermes 的增量 resume。
	needReset := modelChanged || s.takePendingReset(conv)
	go s.dispatch(context.Background(), conv, input, atts, needReset)

	return nil
}

// dispatch 签发 TempLLMKey + 选 executor + 下发 a2a_request。
//
// attachments 是**本轮**新上传的附件；历史轮次的附件在下面从各条 message 的
// metadata 里重新读出，两者来源不同，不能混为一谈。
// sessionReset 为 true 时通知 executor 丢弃 hermes 侧缓存的 session。
// 模型/provider 变更必须置 true —— hermes 的模型是建 session 时定的，
// 只 resume 会继续用旧模型，用户改了模型却毫无变化。
func (s *Service) dispatch(ctx context.Context, conv *model.Conversation, input string, attachments []a2a.Attachment, sessionReset bool) {
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
	// 拉对话历史消息，放进 Context（让 executor 无状态，容器可重建后从 core 恢复）。
	var history []model.Message
	global.PRISM_DB.Where("conversation_id = ? AND msg_type IN ?", conv.ID,
		[]string{"text", "result"}).Order("id ASC").Find(&history)
	hist := make([]a2a.HistoryMessage, 0, len(history))
	for _, m := range history {
		role := "user"
		if m.SenderType != "user" {
			role = "assistant"
		}
		// 历史消息里的附件也要带上并重新签 URL，否则第 1 轮传的文件到第 2 轮
		// 就从 agent 视野里消失了（hermes 侧 session 重建时按这份历史恢复）。
		histAtts := attachmentsFromMetadata(m.Metadata)
		signAttachments(ctx, histAtts)
		hist = append(hist, a2a.HistoryMessage{
			Role:        role,
			Content:     m.Content,
			Attachments: histAtts,
		})
	}
	// 对象形态（见 a2a.ExecutionContext）。executor 侧用 DecodeExecutionContext
	// 兼容读取，故新老版本可以不同步部署。
	histJSON, _ := json.Marshal(a2a.ExecutionContext{History: hist})

	// 本轮附件现签现用（URL 有时效，不落库）。
	signAttachments(ctx, attachments)

	// 只有对话真的选了 provider 才下发对话级 key。
	//
	// providerID=0 时签出来的 key 是**不可用的**：llmproxy 按 providerID 查库，
	// provider 0 不存在 → 500 "llm provider unavailable"。此前这个坏 key 也在下发，
	// 但 executor 从不读它（一律用服务级 key），所以问题被掩盖着；改成优先用对话级
	// key 之后它就暴露成"不选模型就报错"。不下发即让 executor 回退服务级兜底。
	execHeaders := map[string]string{}
	if providerID != 0 {
		execHeaders[llm.KeyHeader] = tempKey.Key
	}
	if sessionReset {
		// 模型变了：要求 executor 丢弃 hermes 侧已缓存的 session。
		// hermes 的模型在建 session 时固化，只 resume 会继续用旧模型。
		// 历史随 Context 全量重注，不丢上下文。
		execHeaders[headerSessionReset] = "1"
	}

	execReq := a2a.ExecutionRequest{
		ConversationID: conv.ID,
		StepID:         stepID,
		Mode:           conv.Mode,
		ProviderID:     conv.ProviderID,
		Model:          conv.Model,
		Input:          input,
		Context:        histJSON, // 对话历史（executor 注入 hermes session）
		Attachments:    attachments,
		Headers:        execHeaders,
	}
	body, _ := json.Marshal(execReq)
	reqEnv, _ := a2a.NewEnvelopeWithRequest(time.Now().UnixMilli(), a2a.EnvA2ARequest, delegationID, a2a.A2ARequestPayload{
		Method:     "message/send",
		Capability: rs.ExecBackend,
		// 与 execReq.Headers 保持一致：providerID=0 时不下发不可用的 key。
		// executor runtime 会把 payload.Headers 作为 fallback 合并进 execReq.Headers，
		// 这里若仍带上，上面的判断就白做了。
		Headers: execHeaders,
		Body:    body,
		Stream:  true,
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

// reqBackendForMode 按 mode 选执行后端。
// 默认走 hermes（Hermes Agent 桥接已接入）；mock-llm 仅协议联调时手动切。
func reqBackendForMode(mode string) string {
	return "hermes"
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
	Mode        string            `json:"mode" required:"true"`
	Input       string            `json:"input" required:"true"`
	ProviderID  *uint             `json:"providerId,omitempty"`
	Model       string            `json:"model,omitempty"`
	AgentID     *uint             `json:"agentId,omitempty"`
	ProjectID   *uint             `json:"projectId,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	Attachments []AttachmentInput `json:"attachments,omitempty" doc:"附件引用（先经 storage 上传拿到 fileId）"`
}

// 确保实现 executorreg.Handler。
var _ executorreg.Handler = (*Service)(nil)
