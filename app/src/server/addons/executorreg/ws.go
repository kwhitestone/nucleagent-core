package executorreg

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nucleagent/nucleagent-shared/a2a"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kwhitestone/prism-fusion/global"
)

// upgrader WS 升级器。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024,
	WriteBufferSize: 64 * 1024,
	// v1 允许所有来源（CORS 由 gin 中间件层处理；S2S 走 X-Executor-Token 鉴权）。
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler 处理 core 下发 + executor 上报的回调集合（由 conversation/a2a 注入）。
type Handler interface {
	// OnStreamEvent executor 上报流式事件。
	OnStreamEvent(env *a2a.Envelope, p a2a.A2AStreamEventPayload)
	// OnTaskResult executor 上报最终结果。
	OnTaskResult(env *a2a.Envelope, p a2a.A2ATaskResultPayload)
	// OnHeartbeat executor 心跳批量上报。
	OnHeartbeat(env *a2a.Envelope, p a2a.A2AHeartbeatBatchPayload)
}

// conn 一个 executor WS 连接的实现。
type conn struct {
	ws         *websocket.Conn
	deviceID   string
	instanceID string

	writeMu sync.Mutex

	// 分块重组缓冲。
	chunkMu sync.Mutex
	chunks  map[string][]*a2a.Envelope

	handler Handler

	// onHandshake 在 handshake 完成后调用一次（用于注册到 hub）。
	onHandshake func()
}

// ServeWS 处理 WS 升级 + 读循环。
//
// executorToken 校验 X-Executor-Token（query 或 header）。
func ServeWS(w http.ResponseWriter, r *http.Request, executorToken string, handler Handler) {
	token := r.Header.Get("X-Executor-Token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if executorToken != "" && token != executorToken {
		http.Error(w, "invalid executor token", http.StatusUnauthorized)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		global.PRISM_LOG.Warn("ws upgrade failed", zap.Error(err))
		return
	}

	c := &conn{
		ws:      ws,
		handler: handler,
		chunks:  make(map[string][]*a2a.Envelope),
	}
	c.onHandshake = func() {
		// handshake 完成回调：此时 c.deviceID/instanceID 已确定，注册到 hub。
		Default.Register(c)
	}
	defer func() {
		// 仅当已注册（handshake 完成）才注销，避免注销空键占位。
		if c.deviceID != "" || c.instanceID != "" {
			Default.Unregister(c)
		}
		ws.Close()
	}()

	// 读循环：等 handshake（首条消息）确定 device/instance id 后注册到 hub，然后持续读。
	c.readLoop(r.Context())
}

// DeviceID / InstanceID 实现 Connection 接口。
func (c *conn) DeviceID() string   { return c.deviceID }
func (c *conn) InstanceID() string { return c.instanceID }

// Send 发送信封（含分块编码）给 executor。
func (c *conn) Send(env *a2a.Envelope) error {
	frames, err := a2a.EncodeEnvelopeFrames(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for _, f := range frames {
		if err := c.ws.WriteMessage(websocket.TextMessage, f); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭连接。
func (c *conn) Close() error { return c.ws.Close() }

// readLoop 持续读取并处理信封。handshake 完成后调 c.onHandshake 注册到 hub。
func (c *conn) readLoop(ctx context.Context) {
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				global.PRISM_LOG.Debug("ws read ended", zap.Error(err))
			}
			return
		}
		var env a2a.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		// 分块重组。
		if env.ChunkID != "" {
			c.handleChunk(&env)
			continue
		}
		c.dispatch(&env)
	}
}

// dispatch 把完整信封按类型分发给 handler。
func (c *conn) dispatch(env *a2a.Envelope) {
	switch env.Type {
	case a2a.EnvHandshake:
		c.applyHandshake(env)
	case a2a.EnvA2AStreamEvent:
		if c.handler == nil {
			return
		}
		var p a2a.A2AStreamEventPayload
		if err := env.ParsePayload(&p); err == nil {
			c.handler.OnStreamEvent(env, p)
		}
	case a2a.EnvA2ATaskResult:
		if c.handler == nil {
			return
		}
		var p a2a.A2ATaskResultPayload
		if err := env.ParsePayload(&p); err == nil {
			c.handler.OnTaskResult(env, p)
		}
	case a2a.EnvA2AHeartbeatBatch:
		if c.handler == nil {
			return
		}
		var p a2a.A2AHeartbeatBatchPayload
		if err := env.ParsePayload(&p); err == nil {
			c.handler.OnHeartbeat(env, p)
		}
	case a2a.EnvPing:
		// 回 pong。
		var p a2a.PingPayload
		_ = env.ParsePayload(&p)
		if pong, err := a2a.NewEnvelopeNow(a2a.EnvPong, a2a.PongPayload{SentAt: p.SentAt, ReceivedAt: time.Now().UnixMilli()}); err == nil {
			_ = c.Send(pong)
		}
	default:
		// a2a_response / task_result_ack / pong / error：v1 不处理。
	}
}

// applyHandshake 从 handshake 信封提取 device/instance id。
func (c *conn) applyHandshake(env *a2a.Envelope) {
	var p a2a.HandshakePayload
	if err := env.ParsePayload(&p); err != nil {
		return
	}
	c.deviceID = p.DeviceID
	if p.InstanceID == "" {
		c.instanceID = uuid.NewString()
	} else {
		c.instanceID = p.InstanceID
	}
	// handshake 完成，触发注册回调（此时 deviceID/instanceID 已确定，注册到 hub）。
	if c.onHandshake != nil {
		c.onHandshake()
	}

	// 回 handshake_ack。
	if ack, err := a2a.NewEnvelopeNow(a2a.EnvHandshakeAck, a2a.HandshakeAckPayload{Status: "ok"}); err == nil {
		_ = c.Send(ack)
	}
	global.PRISM_LOG.Info("executor handshake",
		zap.String("device", c.deviceID),
		zap.String("instance", c.instanceID))
}

// handleChunk 分块重组。
func (c *conn) handleChunk(env *a2a.Envelope) {
	c.chunkMu.Lock()
	c.chunks[env.ChunkID] = append(c.chunks[env.ChunkID], env)
	if len(c.chunks[env.ChunkID]) < env.ChunkTotal {
		c.chunkMu.Unlock()
		return
	}
	parts := c.chunks[env.ChunkID]
	delete(c.chunks, env.ChunkID)
	c.chunkMu.Unlock()

	complete, _, err := a2a.DecodeEnvelopeFrames(parts)
	if err != nil || len(complete) == 0 {
		return
	}
	c.dispatch(complete[0])
}
