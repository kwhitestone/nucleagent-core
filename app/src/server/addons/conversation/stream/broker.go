// Package stream core 侧消息事件 broker：进程内 pub/sub + 50ms 同 ID 合并。
//
// 参考 agentia-engine/src/service/taskstream/broker.go（附录 §7.2）：
//   - streaming 每 token flush 会在同一 message ID 上产生数百次 Updated 事件
//   - broker 对同一 message ID 的 Updated 事件在 50ms 窗口内合并为一次（只投递最新快照）
//   - Created/Deleted/Status 永不合并
//   - 慢订阅者 drop（行已在 DB，前端重连用 Last-Event-ID 补齐）
//
// v1 单实例：进程内 fan-out。多实例后置加 Redis pub/sub。
package stream

import (
	"sync"
	"time"

	"github.com/nucleagent/nucleagent-shared/model"
)

// 事件类型。
type Kind int

const (
	KindCreated Kind = iota
	KindUpdated
	KindDeleted
	KindStatus
)

// Event 推给订阅者的事件。
type Event struct {
	Kind        Kind
	ConversationID uint
	Message      *model.Message // Created/Updated 携带全行快照
	MessageID    uint            // Deleted 携带 id（行已删）
}

// subscriber 一个订阅者。
type subscriber struct {
	ch     chan Event
	cancel chan struct{}
}

// Broker 进程内事件 broker。
type Broker struct {
	mu          sync.RWMutex
	subs        map[uint][]*subscriber // conversationID -> 订阅者
	coalesceMu  sync.Mutex
	pending     map[uint]*coalesceEntry // messageID -> 待合并的 Updated 最新快照
	coalesceDur time.Duration
}

type coalesceEntry struct {
	convID   uint
	msg      *model.Message
	timer    *time.Timer
}

// NewBroker 构造 broker。coalesceDur 是 Updated 合并窗口（默认 50ms）。
func NewBroker(coalesceDur time.Duration) *Broker {
	if coalesceDur <= 0 {
		coalesceDur = 50 * time.Millisecond
	}
	return &Broker{
		subs:        make(map[uint][]*subscriber),
		pending:     make(map[uint]*coalesceEntry),
		coalesceDur: coalesceDur,
	}
}

// Default 全局 broker。
var Default = NewBroker(50 * time.Millisecond)

// Subscribe 订阅某 conversation 的事件。返回事件 channel 和取消函数。
// channel 缓冲 64；满时 drop 事件（前端重连补齐）。
func (b *Broker) Subscribe(convID uint) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := &subscriber{
		ch:     make(chan Event, 64),
		cancel: make(chan struct{}),
	}
	b.subs[convID] = append(b.subs[convID], s)
	return s.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[convID]
		for i, ss := range subs {
			if ss == s {
				b.subs[convID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(s.cancel)
	}
}

// PublishCreated 发布 Created 事件（不合并，立即投递）。
func (b *Broker) PublishCreated(convID uint, msg *model.Message) {
	b.dispatch(convID, Event{Kind: KindCreated, ConversationID: convID, Message: msg})
}

// PublishUpdated 发布 Updated 事件（同 messageID 50ms 内合并）。
func (b *Broker) PublishUpdated(convID uint, msg *model.Message) {
	if msg == nil {
		return
	}
	b.coalesceMu.Lock()
	defer b.coalesceMu.Unlock()
	if existing, ok := b.pending[msg.ID]; ok {
		// 已有待合并项，更新快照（timer 不变）。
		existing.msg = msg
		return
	}
	entry := &coalesceEntry{convID: convID, msg: msg}
	entry.timer = time.AfterFunc(b.coalesceDur, func() {
		b.coalesceMu.Lock()
		cur := b.pending[msg.ID]
		delete(b.pending, msg.ID)
		b.coalesceMu.Unlock()
		if cur != nil {
			b.dispatch(cur.convID, Event{Kind: KindUpdated, ConversationID: cur.convID, Message: cur.msg})
		}
	})
	b.pending[msg.ID] = entry
}

// PublishDeleted 发布 Deleted 事件（不合并）。
func (b *Broker) PublishDeleted(convID, msgID uint) {
	b.dispatch(convID, Event{Kind: KindDeleted, ConversationID: convID, MessageID: msgID})
}

// PublishStatus 发布 Status 事件（不合并）。
func (b *Broker) PublishStatus(convID uint, msg *model.Message) {
	b.dispatch(convID, Event{Kind: KindStatus, ConversationID: convID, Message: msg})
}

// dispatch 把事件投递给该 conversation 的所有订阅者。
// 满则 drop（非阻塞）。
func (b *Broker) dispatch(convID uint, ev Event) {
	b.mu.RLock()
	subs := make([]*subscriber, len(b.subs[convID]))
	copy(subs, b.subs[convID])
	b.mu.RUnlock()
	for _, s := range subs {
		select {
		case s.ch <- ev:
		case <-s.cancel:
		default:
			// drop：订阅者缓冲满，前端重连补齐。
		}
	}
}
