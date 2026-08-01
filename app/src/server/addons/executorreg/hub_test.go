package executorreg

import (
	"sync"
	"testing"

	"github.com/nucleagent/nucleagent-shared/a2a"
)

// fakeConn 测试用 Connection。
type fakeConn struct {
	deviceID   string
	instanceID string
	sent       []*a2a.Envelope
	mu         sync.Mutex
}

func (f *fakeConn) Send(env *a2a.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, env)
	return nil
}
func (f *fakeConn) DeviceID() string   { return f.deviceID }
func (f *fakeConn) InstanceID() string { return f.instanceID }
func (f *fakeConn) Close() error       { return nil }

// TestHubRegisterPick 验证注册后能 Pick 到。
func TestHubRegisterPick(t *testing.T) {
	h := NewHub()
	c := &fakeConn{deviceID: "dev-1", instanceID: "inst-1"}
	h.Register(c)

	got, ok := h.Pick("dev-1")
	if !ok {
		t.Fatal("Pick dev-1: not found")
	}
	if got.InstanceID() != "inst-1" {
		t.Errorf("got instance %q, want inst-1", got.InstanceID())
	}
	if h.Count() != 1 {
		t.Errorf("Count = %d, want 1", h.Count())
	}
}

// TestHubPickAnyDevice 验证 deviceID 为空时取任意一个。
func TestHubPickAnyDevice(t *testing.T) {
	h := NewHub()
	h.Register(&fakeConn{deviceID: "dev-1", instanceID: "i1"})

	got, ok := h.Pick("")
	if !ok {
		t.Fatal("Pick empty: not found")
	}
	if got.DeviceID() != "dev-1" {
		t.Errorf("got device %q", got.DeviceID())
	}
}

// TestHubPickMissing 验证无连接时 Pick 返回 false。
func TestHubPickMissing(t *testing.T) {
	h := NewHub()
	if _, ok := h.Pick("nope"); ok {
		t.Error("Pick on empty hub should return false")
	}
	if _, ok := h.Pick(""); ok {
		t.Error("Pick empty on empty hub should return false")
	}
}

// TestHubUnregister 验证注销后 Pick 不到。
func TestHubUnregister(t *testing.T) {
	h := NewHub()
	c := &fakeConn{deviceID: "dev-1", instanceID: "inst-1"}
	h.Register(c)
	h.Unregister(c)

	if _, ok := h.Pick("dev-1"); ok {
		t.Error("Pick after Unregister should fail")
	}
	if h.Count() != 0 {
		t.Errorf("Count = %d, want 0", h.Count())
	}
}

// TestHubMultipleInstancesSameDevice 验证多实例同 device 分组。
func TestHubMultipleInstancesSameDevice(t *testing.T) {
	h := NewHub()
	h.Register(&fakeConn{deviceID: "dev-1", instanceID: "i1"})
	h.Register(&fakeConn{deviceID: "dev-1", instanceID: "i2"})
	h.Register(&fakeConn{deviceID: "dev-2", instanceID: "i3"})

	if h.Count() != 3 {
		t.Errorf("Count = %d, want 3", h.Count())
	}
	devs := h.ListDeviceIDs()
	if len(devs) != 2 {
		t.Errorf("device count = %d, want 2", len(devs))
	}

	// Pick dev-1 应返回 dev-1 的某个实例。
	got, ok := h.Pick("dev-1")
	if !ok || got.DeviceID() != "dev-1" {
		t.Errorf("Pick dev-1 wrong: %+v", got)
	}

	// 注销 i1 后，dev-1 还有 i2。
	h.Unregister(&fakeConn{deviceID: "dev-1", instanceID: "i1"})
	if _, ok := h.Pick("dev-1"); !ok {
		t.Error("Pick dev-1 after removing one instance should still find i2")
	}

	// 注销 i2 后，dev-1 空了。
	h.Unregister(&fakeConn{deviceID: "dev-1", instanceID: "i2"})
	if _, ok := h.Pick("dev-1"); ok {
		t.Error("Pick dev-1 after removing all instances should fail")
	}
	// dev-2 还在。
	if _, ok := h.Pick("dev-2"); !ok {
		t.Error("dev-2 should still be registered")
	}
}

// TestHubRegisterEmptyInstanceID 验证空 instanceID 时仍能注册（Pick 按实例 id 取）。
func TestHubRegisterEmptyInstanceID(t *testing.T) {
	h := NewHub()
	c := &fakeConn{deviceID: "dev-1", instanceID: ""}
	h.Register(c)
	// 空 instanceID 注册到 "" 键，Pick dev-1 能通过 byDevice 索引找到。
	got, ok := h.Pick("dev-1")
	if !ok {
		t.Fatal("Pick dev-1: not found despite empty instance")
	}
	_ = got
}
