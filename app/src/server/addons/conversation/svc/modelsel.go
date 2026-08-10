// Package svc 的模型/提供商选择。
//
// 为什么模型不能只用名字定位：llmproxy 按 **providerID** 查库解密 API key
// （见 llmproxy.ResolveProvider），同名模型可能挂在不同 provider 下，
// 光有模型名无法确定用谁的凭据。所以选择始终是 (providerID, model) 一对。
package svc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nucleagent/nucleagent-shared/model"
	"go.uber.org/zap"
	"whitestone.top/prism-fusion/global"

	"nucleagent-core/addons/llmproxy"
)

// ErrInvalidModel 模型/提供商选择不合法（客户端问题 → 4xx）。
var ErrInvalidModel = errors.New("模型选择不可用")

// headerSessionReset 通知 executor 丢弃 hermes 侧缓存 session 的信号头。
//
// 必须与 executor 侧 hermes.HeaderSessionReset 的值一致。走 Headers 而不是新增
// ExecutionRequest 字段：这是「本次执行的一个指令」而非任务数据，且旧版 executor
// 收到未知头会自然忽略，无需协议协商。
const headerSessionReset = "x-session-reset"

// ModelSelection 一次模型选择。两个字段都可选：
//   - 只给 Model：沿用对话现有 provider，仅换模型
//   - 都给：同时换 provider 与模型
//   - 都不给（或整体为 nil）：不改动
type ModelSelection struct {
	ProviderID *uint  `json:"providerId,omitempty" doc:"LLM 提供商 ID（省略则沿用对话现值）"`
	Model      string `json:"model,omitempty" doc:"模型名，须在该提供商的可用列表内"`
}

// isEmpty 判断这次选择是否什么都没指定。
func (m *ModelSelection) isEmpty() bool {
	return m == nil || (m.ProviderID == nil && strings.TrimSpace(m.Model) == "")
}

// ValidateSelection 校验选择可用：provider 存在且启用、模型在其白名单内。
//
// 复用 llmproxy.ValidateModel —— 白名单规则只留一份，避免业务层与 proxy 层
// 各写一套后规则漂移（那会导致 UI 放行、真正请求时被 proxy 拒掉）。
func ValidateSelection(providerID uint, modelName string) error {
	modelName = strings.TrimSpace(modelName)
	if providerID == 0 && modelName == "" {
		return nil
	}
	if providerID == 0 {
		// 有模型名却没有 provider：无法定位凭据，必须拒。
		return fmt.Errorf("%w: 指定模型时必须同时指定 providerId", ErrInvalidModel)
	}
	if global.PRISM_DB == nil {
		return fmt.Errorf("数据库未初始化")
	}
	var p model.Provider
	if err := global.PRISM_DB.First(&p, providerID).Error; err != nil {
		return fmt.Errorf("%w: 提供商 %d 不存在", ErrInvalidModel, providerID)
	}
	if !p.IsActive {
		return fmt.Errorf("%w: 提供商 %s 已停用", ErrInvalidModel, p.Name)
	}
	if err := llmproxy.ValidateModel(providerID, modelName); err != nil {
		if errors.Is(err, llmproxy.ErrModelNotAllowed) {
			return fmt.Errorf("%w: %v", ErrInvalidModel, err)
		}
		return err
	}
	return nil
}

// stateKeyPendingReset conversation.state 里「待重建 hermes session」的标记 key。
//
// 为什么必须持久化，不能只靠本次请求内的「模型是否变化」：
// PATCH 切换模型时已经把新模型写进库了，等到下一轮 follow-up 再比对时
// 已经看不出差异（库里就是新值），于是不会发重置信号 —— hermes 继续 resume
// 那个用旧模型建的 session，用户改了模型却毫无变化。实测踩过这个坑。
//
// 落在已有的 state JSON 列上（无需迁移），且能跨 core 重启存活。
const stateKeyPendingReset = "pending_session_reset"

// UpdateModel 切换对话的模型/提供商（供 PATCH 端点用）。
//
// 只落库，不触发执行 —— 新模型在**下一轮** follow-up 时生效。
// 不在这里重建 hermes session：那要发一次 a2a_request，而此刻没有用户输入可发；
// 改为打一个持久化标记，交给下一轮 dispatch 消费（见 takePendingReset）。
func (s *Service) UpdateModel(conv *model.Conversation, sel *ModelSelection) error {
	if sel.isEmpty() {
		return fmt.Errorf("%w: 未指定 providerId 或 model", ErrInvalidModel)
	}
	changed, err := s.applyModelSelection(conv, sel)
	if err != nil {
		return err
	}
	if changed {
		return s.markPendingReset(conv)
	}
	return nil
}

// markPendingReset 打上「下一轮需重建 session」标记。
func (s *Service) markPendingReset(conv *model.Conversation) error {
	state := map[string]any{}
	if len(conv.State) > 0 {
		// 解析失败不致命：宁可覆盖成只含本标记的 state，也不能因为一条脏数据
		// 让模型切换永久失效（state 目前没有其它使用者）。
		_ = json.Unmarshal(conv.State, &state)
	}
	state[stateKeyPendingReset] = true
	next := model.MustNewJSON(state)
	if err := global.PRISM_DB.Model(&model.Conversation{}).
		Where("id = ?", conv.ID).Update("state", next).Error; err != nil {
		return fmt.Errorf("标记 session 重建失败: %w", err)
	}
	conv.State = next
	return nil
}

// takePendingReset 读取并清除「待重建 session」标记（消费一次即失效）。
//
// 清除必须与读取一起做：否则标记会一直留着，之后每轮都白重建 session，
// 白扔掉 hermes 增量 resume 的全部收益。
func (s *Service) takePendingReset(conv *model.Conversation) bool {
	if len(conv.State) == 0 {
		return false
	}
	state := map[string]any{}
	if err := json.Unmarshal(conv.State, &state); err != nil {
		return false
	}
	v, ok := state[stateKeyPendingReset].(bool)
	if !ok || !v {
		return false
	}
	delete(state, stateKeyPendingReset)
	next := model.MustNewJSON(state)
	if err := global.PRISM_DB.Model(&model.Conversation{}).
		Where("id = ?", conv.ID).Update("state", next).Error; err != nil {
		global.PRISM_LOG.Warn("conversation: 清除 session 重建标记失败", zap.Error(err))
	}
	conv.State = next
	return true
}

// applyModelSelection 校验并把选择写进 conversation 行。
//
// 返回 changed 表示模型或 provider 实际发生了变化 —— 调用方据此决定是否要求
// executor 重建 hermes session（hermes 的模型在建 session 时固化，不重建改不掉）。
// 选了但与现值相同则 changed=false，避免白重建、白丢增量 resume。
func (s *Service) applyModelSelection(conv *model.Conversation, sel *ModelSelection) (bool, error) {
	if sel.isEmpty() {
		return false, nil
	}

	// 目标值：未指定的字段沿用对话现值。
	curProvider := uint(0)
	if conv.ProviderID != nil {
		curProvider = *conv.ProviderID
	}
	targetProvider := curProvider
	if sel.ProviderID != nil {
		targetProvider = *sel.ProviderID
	}
	targetModel := strings.TrimSpace(sel.Model)
	if targetModel == "" {
		targetModel = conv.Model
	}

	if err := ValidateSelection(targetProvider, targetModel); err != nil {
		return false, err
	}

	if targetProvider == curProvider && targetModel == conv.Model {
		return false, nil
	}

	updates := map[string]any{"model": targetModel}
	if targetProvider != 0 {
		updates["provider_id"] = targetProvider
	}
	if err := global.PRISM_DB.Model(&model.Conversation{}).
		Where("id = ?", conv.ID).Updates(updates).Error; err != nil {
		return false, fmt.Errorf("更新对话模型失败: %w", err)
	}

	// 同步内存副本：dispatch 紧接着就按 conv 的字段签 key / 组 execReq，
	// 不同步会让本轮仍用旧模型（表现为「切换慢一拍」）。
	conv.Model = targetModel
	if targetProvider != 0 {
		p := targetProvider
		conv.ProviderID = &p
	}

	global.PRISM_LOG.Info("conversation: 模型已切换",
		zap.Uint("conv", conv.ID), zap.Uint("providerId", targetProvider),
		zap.String("model", targetModel))
	return true, nil
}
