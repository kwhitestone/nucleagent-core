// Package executorreg core 侧 Executor 注册 + WebSocket 服务端。
//
// 参考 agentia-engine/src/service/desktop/ + executor_registry.go + router/desktop_ws_routes.go：
//   - POST /api/v1/addons/s2s/executor/register：executor 注册，返回 wsUrl
//   - WS 服务端：accept 连接 -> handshake -> 维护 device 注册表
//   - core -> executor 下发 a2a_request / task_kill
//   - executor -> core 上报 a2a_stream_event / a2a_task_result / heartbeat
//
// v1 单实例：device 注册表用内存。多实例灰度后置（附录 §7.5）。
package executorreg

import (
	"sync"
	"time"

	"github.com/nucleagent/nucleagent-shared/a2a"
)

// Connection 一个已连接的 executor WebSocket 连接抽象。
type Connection interface {
	// Send 发送信封（已含分块编码）。
	Send(env *a2a.Envelope) error
	// DeviceID 该连接的 device id。
	DeviceID() string
	// InstanceID 实例 id。
	InstanceID() string
	// Close 关闭连接。
	Close() error
}

// Hub 维护已注册的 executor 连接，按 device/instance 索引。
type Hub struct {
	mu          sync.RWMutex
	connections map[string]Connection // instanceID -> Connection
	byDevice    map[string][]string  // deviceID -> []instanceID
}

// NewHub 构造空 hub。
func NewHub() *Hub {
	return &Hub{
		connections: make(map[string]Connection),
		byDevice:    make(map[string][]string),
	}
}

// Default 全局 hub（core 单实例用）。
var Default = NewHub()

// Register 注册一个连接。
// 连接的 DeviceID/InstanceID 在 handshake 前可能为空；本方法要求调用方在
// 确定身份后再注册（避免空键占位注册被孤立）。
func (h *Hub) Register(c Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inst := c.InstanceID()
	h.connections[inst] = c
	h.byDevice[c.DeviceID()] = append(h.byDevice[c.DeviceID()], inst)
}

// Unregister 注销连接。
func (h *Hub) Unregister(c Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inst := c.InstanceID()
	delete(h.connections, inst)
	insts := h.byDevice[c.DeviceID()]
	for i, id := range insts {
		if id == inst {
			h.byDevice[c.DeviceID()] = append(insts[:i], insts[i+1:]...)
			break
		}
	}
	if len(h.byDevice[c.DeviceID()]) == 0 {
		delete(h.byDevice, c.DeviceID())
	}
}

// Pick 按 deviceID 选一个连接（v1 简单轮询：取第一个）。
// deviceID 非空时只从该 device 选，找不到返回 false（不回退到其他 device）。
// deviceID 为空时取任意一个。
func (h *Hub) Pick(deviceID string) (Connection, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if deviceID != "" {
		insts := h.byDevice[deviceID]
		for _, inst := range insts {
			if c, ok := h.connections[inst]; ok {
				return c, true
			}
		}
		// 指定了 deviceID 但没找到，不回退。
		return nil, false
	}
	for _, c := range h.connections {
		return c, true
	}
	return nil, false
}

// Count 已连接实例数。
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// ListDeviceIDs 列出所有 device id。
func (h *Hub) ListDeviceIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.byDevice))
	for d := range h.byDevice {
		out = append(out, d)
	}
	return out
}

// RegisteredDevice 注册响应里给 executor 的设备信息。
type RegisteredDevice struct {
	DeviceID     string    `json:"deviceId"`
	InstanceID   string    `json:"instanceId"`
	WSURL        string    `json:"wsUrl"`
	RegisteredAt time.Time `json:"registeredAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}
