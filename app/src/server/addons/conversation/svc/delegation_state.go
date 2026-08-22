// delegation_state.go 后台委托「在飞」标志的持久化（conversation.state JSON 列）。
//
// executor 是无状态的：它靠 a2a_request 的 x-delegation-watch 头知道本轮结束后
// 要不要接管 delegation watcher。标志存 core DB —— core / executor 任一重启都不丢，
// 下一个到达该对话的 Run 自然接回监听。
package svc

import (
	"encoding/json"

	"github.com/nucleagent/nucleagent-shared/model"

	"github.com/kwhitestone/prism-fusion/global"
)

// headerDelegationWatch 通知 executor 本轮结束后接管 delegation watcher 的信号头。
const headerDelegationWatch = "x-delegation-watch"

// stateKeyDelegationPending conversation.state 里的键：有后台委托未完成。
const stateKeyDelegationPending = "delegationPending"

// markDelegationPending 置「在飞后台委托」标志（dispatch 下发 x-delegation-watch 的依据）。
func markDelegationPending(convID uint) {
	updateDelegationState(convID, true)
}

// clearDelegationPending 清「在飞后台委托」标志（turn 2 完成且无链式委托时调）。
func clearDelegationPending(convID uint) {
	updateDelegationState(convID, false)
}

// delegationPendingInDB 查询标志（读快照，不修改）。
func delegationPendingInDB(convID uint) bool {
	var conv model.Conversation
	if err := global.PRISM_DB.Select("state").First(&conv, "id = ?", convID).Error; err != nil {
		return false
	}
	if len(conv.State) == 0 {
		return false
	}
	state := map[string]any{}
	if err := json.Unmarshal(conv.State, &state); err != nil {
		return false
	}
	v, _ := state[stateKeyDelegationPending].(bool)
	return v
}

// updateDelegationState 原子更新标志（读-改-写，state 列目前无并发写者）。
func updateDelegationState(convID uint, pending bool) {
	var conv model.Conversation
	if err := global.PRISM_DB.Select("state").First(&conv, "id = ?", convID).Error; err != nil {
		return
	}
	state := map[string]any{}
	if len(conv.State) > 0 {
		// 解析失败不致命：覆盖成本只是丢掉 state 里其它键，而 delegation 标志
		// 本身就是新写的，宁可重写也不能因脏数据卡死。
		_ = json.Unmarshal(conv.State, &state)
	}
	if pending {
		state[stateKeyDelegationPending] = true
	} else {
		delete(state, stateKeyDelegationPending)
	}
	next := model.MustNewJSON(state)
	_ = global.PRISM_DB.Model(&model.Conversation{}).
		Where("id = ?", convID).Update("state", next).Error
}
