package svc

import (
	"testing"

	"github.com/nucleagent/nucleagent-shared/model"
	"nucleagent-core/addons/conversation/stream"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"github.com/kwhitestone/prism-fusion/global"
)

// setupTestDB 初始化内存 sqlite 并迁移 Message 表。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Exec("DELETE FROM messages")
	global.PRISM_DB = db
	return db
}

// TestStreamUpsertCreatesThenUpdates 验证首个 delta 创建 streaming 行，后续 delta 就地更新。
func TestStreamUpsertCreatesThenUpdates(t *testing.T) {
	db := setupTestDB(t)
	broker := stream.NewBroker(0) // 0 -> 用默认 50ms

	// 第一个 delta：创建。
	msg1, err := streamUpsert(db, broker, 1, "step-1", "deleg-1", "agent", "AI助手", "Hello")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if msg1.MsgType != model.MsgTypeStreaming || msg1.Content != "Hello" {
		t.Errorf("first upsert wrong: %+v", msg1)
	}

	// 第二个 delta（同 key）：就地更新（content 替换）。
	msg2, err := streamUpsert(db, broker, 1, "step-1", "deleg-1", "agent", "AI助手", "Hello World")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if msg2.ID != msg1.ID {
		t.Errorf("expected same row id, got %d vs %d", msg2.ID, msg1.ID)
	}
	if msg2.Content != "Hello World" {
		t.Errorf("content = %q, want Hello World", msg2.Content)
	}

	// 验证只有一行 streaming。
	var count int64
	db.Model(&model.Message{}).Where("conversation_id = ? AND msg_type = ?", 1, model.MsgTypeStreaming).Count(&count)
	if count != 1 {
		t.Errorf("streaming row count = %d, want 1", count)
	}
}

// TestStreamUpsertDifferentKeysCreateSeparateRows 验证不同 (step/delegation) 创建不同行。
func TestStreamUpsertDifferentKeysCreateSeparateRows(t *testing.T) {
	db := setupTestDB(t)
	broker := stream.NewBroker(0)

	// 同 conv，不同 step -> 不同行。
	_, _ = streamUpsert(db, broker, 1, "step-A", "deleg-1", "agent", "AI助手", "A")
	_, _ = streamUpsert(db, broker, 1, "step-B", "deleg-1", "agent", "AI助手", "B")
	// 同 conv，不同 sender slug（thinking）-> 不同行。
	_, _ = streamUpsert(db, broker, 1, "step-A", "deleg-1", "agent.thinking", "AI助手", "think")

	var count int64
	db.Model(&model.Message{}).Where("conversation_id = ? AND msg_type = ?", 1, model.MsgTypeStreaming).Count(&count)
	if count != 3 {
		t.Errorf("expected 3 streaming rows, got %d", count)
	}
}

// TestFinalizeStreamingPromotesRow 验证 streaming 行提升为最终类型。
func TestFinalizeStreamingPromotesRow(t *testing.T) {
	db := setupTestDB(t)
	broker := stream.NewBroker(0)

	// 先创建 streaming 行。
	_, _ = streamUpsert(db, broker, 1, "step-1", "deleg-1", "agent", "AI助手", "partial")
	// finalize 提升为 result。
	if err := finalizeStreaming(db, broker, 1, "step-1", "deleg-1", "agent", model.MsgTypeResult, "final answer"); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	var msg model.Message
	db.Where("conversation_id = ?", 1).First(&msg)
	if msg.MsgType != model.MsgTypeResult {
		t.Errorf("msg_type = %q, want result", msg.MsgType)
	}
	if msg.Content != "final answer" {
		t.Errorf("content = %q, want final answer", msg.Content)
	}
}

// TestFinalizeStreamingNoPlaceholderCreatesFinal 验证无 streaming 占位时直接创建最终消息。
func TestFinalizeStreamingNoPlaceholderCreatesFinal(t *testing.T) {
	db := setupTestDB(t)
	broker := stream.NewBroker(0)

	// 直接 finalize（无前置 streaming）。
	if err := finalizeStreaming(db, broker, 1, "step-X", "deleg-X", "agent", model.MsgTypeResult, "direct result"); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	var count int64
	db.Model(&model.Message{}).Where("conversation_id = ?", 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 message, got %d", count)
	}
	var msg model.Message
	db.First(&msg)
	if msg.MsgType != model.MsgTypeResult {
		t.Errorf("msg_type = %q, want result", msg.MsgType)
	}
}

// TestAppendToStreamingAccumulates 验证 append 累积 content。
func TestAppendToStreamingAccumulates(t *testing.T) {
	db := setupTestDB(t)
	// appendToStreaming 用全局 stream.Default，但测试不想等 50ms；直接测累积逻辑。
	appendToStreaming(db, 1, "step-1", "deleg-1", "agent", "Hello")
	appendToStreaming(db, 1, "step-1", "deleg-1", "agent", " ")
	appendToStreaming(db, 1, "step-1", "deleg-1", "agent", "World")

	var msg model.Message
	db.Where("conversation_id = ?", 1).First(&msg)
	if msg.Content != "Hello World" {
		t.Errorf("accumulated content = %q, want 'Hello World'", msg.Content)
	}

	// 验证只有一行。
	var count int64
	db.Model(&model.Message{}).Where("conversation_id = ?", 1).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 row after appends, got %d", count)
	}
}

// TestAppendToStreamingEmptyDeltaNoOp 验证空 delta 不创建行。
func TestAppendToStreamingEmptyDeltaNoOp(t *testing.T) {
	db := setupTestDB(t)
	appendToStreaming(db, 1, "step-1", "deleg-1", "agent", "")
	var count int64
	db.Model(&model.Message{}).Where("conversation_id = ?", 1).Count(&count)
	if count != 0 {
		t.Errorf("empty delta should create nothing, got %d rows", count)
	}
}
