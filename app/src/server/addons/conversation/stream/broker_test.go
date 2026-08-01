package stream

import (
	"sync"
	"testing"
	"time"

	"github.com/nucleagent/nucleagent-shared/model"
)

// mkMsg 构造测试消息。
func mkMsg(id uint, convID uint, content string) *model.Message {
	return &model.Message{ID: id, ConversationID: convID, Content: content, MsgType: model.MsgTypeStreaming}
}

// TestBrokerCreatedNotCoalesced 验证 Created 事件不合并，立即投递。
func TestBrokerCreatedNotCoalesced(t *testing.T) {
	b := NewBroker(50 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()

	b.PublishCreated(1, mkMsg(1, 1, "a"))
	b.PublishCreated(1, mkMsg(2, 1, "b"))

	// 两个 Created 都应立即收到。
	got := drain(ch, 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("expected 2 created events, got %d", len(got))
	}
	if got[0].Kind != KindCreated || got[1].Kind != KindCreated {
		t.Errorf("expected both KindCreated")
	}
}

// TestBrokerUpdatedCoalesced 验证同 ID 的 Updated 在窗口内合并为一次。
func TestBrokerUpdatedCoalesced(t *testing.T) {
	b := NewBuilder(50 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()

	// 同一 message ID 连续发 5 次 Updated（在 50ms 内）。
	for i := 0; i < 5; i++ {
		b.PublishUpdated(1, mkMsg(10, 1, "v"+string(rune('0'+i))))
	}

	// 等待合并窗口 + 投递。
	got := drain(ch, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected 1 coalesced updated event, got %d", len(got))
	}
	if got[0].Kind != KindUpdated {
		t.Errorf("expected KindUpdated")
	}
	// 合并后应只投递最新快照（v4）。
	if got[0].Message.Content != "v4" {
		t.Errorf("expected latest snapshot v4, got %q", got[0].Message.Content)
	}
}

// TestBrokerUpdatedDifferentIDsNotCoalesced 验证不同 ID 的 Updated 不互相合并。
func TestBrokerUpdatedDifferentIDsNotCoalesced(t *testing.T) {
	b := NewBroker(50 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()

	b.PublishUpdated(1, mkMsg(10, 1, "a"))
	b.PublishUpdated(1, mkMsg(11, 1, "b"))

	got := drain(ch, 2, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("expected 2 updated events (different IDs), got %d", len(got))
	}
}

// TestBrokerUpdatedAcrossWindows 验证跨窗口的 Updated 各投递一次。
func TestBrokerUpdatedAcrossWindows(t *testing.T) {
	b := NewBroker(30 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()

	b.PublishUpdated(1, mkMsg(10, 1, "first"))
	time.Sleep(60 * time.Millisecond) // 等窗口结束 + 投递
	b.PublishUpdated(1, mkMsg(10, 1, "second"))

	got := drain(ch, 2, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("expected 2 updated events across windows, got %d", len(got))
	}
	if got[0].Message.Content != "first" || got[1].Message.Content != "second" {
		t.Errorf("order/content wrong: %q %q", got[0].Message.Content, got[1].Message.Content)
	}
}

// TestBrokerDeletedNotCoalesced 验证 Deleted 立即投递。
func TestBrokerDeletedNotCoalesced(t *testing.T) {
	b := NewBroker(50 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()

	b.PublishDeleted(1, 5)
	got := drain(ch, 1, 200*time.Millisecond)
	if len(got) != 1 || got[0].Kind != KindDeleted || got[0].MessageID != 5 {
		t.Errorf("deleted event wrong: %+v", got)
	}
}

// TestBrokerSubscribeUnsubscribe 验证取消订阅后不再收到事件。
func TestBrokerSubscribeUnsubscribe(t *testing.T) {
	b := NewBroker(50 * time.Millisecond)
	ch, unsub := b.Subscribe(1)

	b.PublishCreated(1, mkMsg(1, 1, "a"))
	drain(ch, 1, time.Second) // 收掉

	unsub()
	b.PublishCreated(1, mkMsg(2, 1, "b"))
	got := drain(ch, 1, 100*time.Millisecond)
	if len(got) != 0 {
		t.Errorf("after unsub, should not receive events, got %d", len(got))
	}
}

// TestBrokerDropOnFullBuffer 验证订阅者缓冲满时 drop（不阻塞发布者）。
func TestBrokerDropOnFullBuffer(t *testing.T) {
	b := NewBroker(50 * time.Millisecond)
	ch, unsub := b.Subscribe(1)
	defer unsub()

	// 缓冲 64，发 100 个 Created，不读取。发布者不应阻塞。
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.PublishCreated(1, mkMsg(uint(i), 1, "x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher blocked on full subscriber buffer")
	}
	// 至少能收到 64 个（缓冲），其余 drop。
	got := drain(ch, 100, 200*time.Millisecond)
	if len(got) > 64 {
		t.Errorf("expected <=64 buffered, got %d", len(got))
	}
}

// drain 从 channel 读取最多 n 个事件，超时返回已读。
func drain(ch <-chan Event, n int, timeout time.Duration) []Event {
	var out []Event
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case ev := <-ch:
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
	return out
}

// NewBuilder 是 NewBroker 的别名（测试用，避免与上面的 b 混淆）。
func NewBuilder(d time.Duration) *Broker { return NewBroker(d) }

// 避免未用 sync 警告（后续扩展用）。
var _ = sync.Mutex{}
