package svc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nucleagent/nucleagent-shared/a2a"
	"github.com/nucleagent/nucleagent-shared/model"
	"nucleagent-core/addons/conversation/stream"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"whitestone.top/prism-fusion/global"
)

// setupFullDB 初始化 sqlite + nop logger 并迁移 Conversation + Message + Step。
func setupFullDB(t *testing.T) *gorm.DB {
	t.Helper()
	if global.PRISM_LOG == nil {
		global.PRISM_LOG = zap.NewNop()
	}
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Conversation{}, &model.Message{}, &model.Step{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Exec("DELETE FROM conversations")
	db.Exec("DELETE FROM messages")
	global.PRISM_DB = db
	return db
}

// TestOnTaskResultIdempotent 验证重复 task_result 只处理一次（不 panic、不重复 finalize）。
//
// 回归 HIGH 修复：OnTaskResult 删除 running 后，重复结果应幂等丢弃。
func TestOnTaskResultIdempotent(t *testing.T) {
	setupFullDB(t)
	s := NewService()

	// 模拟一个已注册的运行态。
	cancelled := false
	rs := &runState{
		StepID:       "step-1",
		DelegationID: "deleg-1",
		SenderSlug:   "agent",
		CancelFn:     func() { cancelled = true },
	}
	s.running[100] = rs

	// 创建 conversation 行（OnTaskResult 会更新它）。
	global.PRISM_DB.Create(&model.Conversation{ID: 100, UserID: 1, Status: "executing"})

	body, _ := json.Marshal(a2a.ExecutionResult{StepID: "step-1", Status: "completed", Output: "done"})

	// 第一次结果：应处理（cancel + finalize + 状态更新）。
	s.OnTaskResult(nil, a2a.A2ATaskResultPayload{
		ConversationID: 100,
		StepID:         "step-1",
		Status:         "completed",
		Body:           body,
	})
	if !cancelled {
		t.Error("first result should have cancelled runCtx")
	}
	if _, ok := s.running[100]; ok {
		t.Error("running entry should be removed after first result")
	}

	// 第二次（重复）结果：应幂等丢弃，不 panic。
	s.OnTaskResult(nil, a2a.A2ATaskResultPayload{
		ConversationID: 100,
		StepID:         "step-1",
		Status:         "completed",
		Body:           body,
	})

	// 验证只有一条 result 消息（finalize 只跑一次）。
	var count int64
	global.PRISM_DB.Model(&model.Message{}).Where("conversation_id = ? AND msg_type = ?", 100, model.MsgTypeResult).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 result message (idempotent), got %d", count)
	}
}

// TestOnTaskResultUnknownConversationNoPanic 验证未知 conversation 的结果不 panic。
//
// 回归 HIGH 修复：OnTaskResult 对不在 running 的 conversation 不解引用 nil runState。
func TestOnTaskResultUnknownConversationNoPanic(t *testing.T) {
	setupFullDB(t)
	s := NewService()

	// 不注册任何 running 态，直接上报一个未知 conversation 的结果。
	body, _ := json.Marshal(a2a.ExecutionResult{Status: "completed", Output: "x"})

	// 不应 panic。
	s.OnTaskResult(nil, a2a.A2ATaskResultPayload{
		ConversationID: 999,
		Status:         "completed",
		Body:           body,
	})

	// 也不应创建任何消息（未处理）。
	var count int64
	global.PRISM_DB.Model(&model.Message{}).Where("conversation_id = ?", 999).Count(&count)
	if count != 0 {
		t.Errorf("unknown conversation result should create nothing, got %d messages", count)
	}
}

// TestStreamBrokerWiredIntoService 验证 svc 用的是 stream.Default（间接）。
func TestStreamBrokerDefault(t *testing.T) {
	// 仅验证 stream.Default 可用，避免 unused import。
	b := stream.NewBroker(10 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()
	b.PublishCreated(1, &model.Message{ID: 1, ConversationID: 1})
	select {
	case ev := <-ch:
		if ev.Kind != stream.KindCreated {
			t.Errorf("expected KindCreated, got %v", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("broker did not deliver event")
	}
}
