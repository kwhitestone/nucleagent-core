// Package svc 的流式消息 upsert 逻辑。
//
// 参考附录 §7.1 + agentia-engine/src/service/a2aorchestrator/a2a_stream_events.go：
//   - 流式 delta 不是每次 append 新消息，而是按 (conversationID, senderSlug, delegationID, stepID)
//     就地 upsert 同一行
//   - 幂等键 a2a-stream:<sha20(conv\0sender\0delegation\0step)> + striped mutex 串行化并发 upsert
//   - 第一个 delta 创建 streaming 行，后续 delta 更新 content
//   - 写库后 publish 全行快照到 broker（Updated 走 50ms 合并）
package svc

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/nucleagent/nucleagent-shared/a2a"
	"github.com/nucleagent/nucleagent-shared/model"
	"nucleagent-core/addons/conversation/stream"

	"gorm.io/gorm"
)

// streamLockStripes striped mutex 桶数（按幂等键 hash 分桶，串行化并发 upsert）。
const streamLockStripes = 256

var streamLocks [streamLockStripes]sync.Mutex

// streamKey 计算流式 upsert 的幂等键。
func streamKey(convID uint, senderSlug, delegationID, stepID string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s", convID, senderSlug, delegationID, stepID)
	return "a2a-stream:" + hex.EncodeToString(h.Sum(nil))
}

// streamLock 返回对应幂等键的锁。
func streamLock(key string) *sync.Mutex {
	h := sha256.Sum256([]byte(key))
	return &streamLocks[int(h[0])%streamLockStripes]
}

// streamUpsert 就地 upsert 一条流式消息。
//
// 命中已有 streaming 行则更新 content + publish Updated；否则创建新 streaming 行 + publish Created。
// 返回最终的消息行。
func streamUpsert(db *gorm.DB, broker *stream.Broker, convID uint, stepID, delegationID, senderSlug, senderName, content string) (*model.Message, error) {
	key := streamKey(convID, senderSlug, delegationID, stepID)
	mu := streamLock(key)
	mu.Lock()
	defer mu.Unlock()

	// 查找已有 streaming 行（dialect 自适应 JSON 查询）。
	var existing model.Message
	sub, subArg := streamKeyClause(db, key)
	err := db.Where("conversation_id = ? AND msg_type = ? AND "+sub,
		convID, model.MsgTypeStreaming, subArg).First(&existing).Error

	if err == nil {
		// 更新 content。
		existing.Content = content
		if err := db.Model(&model.Message{}).Where("id = ?", existing.ID).
			Update("content", content).Error; err != nil {
			return nil, err
		}
		broker.PublishUpdated(convID, &existing)
		return &existing, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// 创建新 streaming 行。
	meta := model.MustNewJSON(map[string]any{
		"step_id":        stepID,
		"delegation_id":  delegationID,
		"stream_key":     key,
		"stream_started": time.Now().UnixMilli(),
	})
	msg := &model.Message{
		ConversationID: convID,
		SenderType:     model.SenderTypeAgent,
		SenderName:     senderName,
		MsgType:        model.MsgTypeStreaming,
		Content:        content,
		Metadata:       meta,
	}
	if err := db.Create(msg).Error; err != nil {
		return nil, err
	}
	broker.PublishCreated(convID, msg)
	return msg, nil
}

// finalizeStreaming 把 streaming 行提升为最终 msg_type（result/text），并清理占位。
func finalizeStreaming(db *gorm.DB, broker *stream.Broker, convID uint, stepID, delegationID, senderSlug, finalType, content string) error {
	key := streamKey(convID, senderSlug, delegationID, stepID)
	mu := streamLock(key)
	mu.Lock()
	defer mu.Unlock()

	var existing model.Message
	sub, subArg := streamKeyClause(db, key)
	err := db.Where("conversation_id = ? AND msg_type = ? AND "+sub,
		convID, model.MsgTypeStreaming, subArg).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		// 没有 streaming 占位，直接创建最终消息。
		msg := &model.Message{
			ConversationID: convID,
			SenderType:     model.SenderTypeAgent,
			SenderName:     senderSlug,
			MsgType:        finalType,
			Content:        content,
			Metadata:       model.MustNewJSON(map[string]any{"step_id": stepID, "delegation_id": delegationID}),
		}
		if err := db.Create(msg).Error; err != nil {
			return err
		}
		broker.PublishCreated(convID, msg)
		return nil
	}
	if err != nil {
		return err
	}
	// 提升为最终类型。
	existing.MsgType = finalType
	existing.Content = content
	if err := db.Model(&model.Message{}).Where("id = ?", existing.ID).
		Updates(map[string]any{"msg_type": finalType, "content": content}).Error; err != nil {
		return err
	}
	broker.PublishUpdated(convID, &existing)
	return nil
}

// dropStreamKey 用于在最终结果时清理 metadata 里的 stream_key（可选）。
var _ = a2a.EnvA2AStreamEvent // 保持 a2a 引用（后续扩展用）
