package executorreg

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"go.uber.org/zap"

	"nucleagent-core/addons/llmproxy"

	"github.com/kwhitestone/prism-fusion/global"
	"github.com/kwhitestone/prism-fusion/plugin"
)

// handler 注入点：conversation/a2a addon 实现 Handler 后注入。
var handler Handler

// SetHandler 注入 core 侧信封处理回调。
func SetHandler(h Handler) { handler = h }

// ExecutorRegPlugin Executor 注册 + WS 服务端插件。
type ExecutorRegPlugin struct {
	plugin.BasePlugin
}

func init() {
	plugin.Register(&ExecutorRegPlugin{
		BasePlugin: plugin.BasePlugin{
			PluginName:        "executor-reg",
			PluginDescription: "Executor 注册 + WebSocket 服务端 + 心跳/租约",
		},
	})
}

func (p *ExecutorRegPlugin) Priority() int { return 25 }

func (p *ExecutorRegPlugin) RoutePrefix() string { return "/api/v1/addons/s2s" }

// RegisterRoutes 注册 HTTP 注册端点 + WS 升级端点 + LLM key 签发端点。
func (p *ExecutorRegPlugin) RegisterRoutes(api huma.API) {
	// HTTP 注册端点（huma）。
	registerHTTPRegister(api)
	// S2S LLM key 签发端点（executor 启动时换服务级长效 key）。
	registerHTTPLLMKey(api)

	// WS 升级端点（huma StreamResponse + humagin.Unwrap 拿 gin context）。
	huma.Register(api, huma.Operation{
		OperationID: "s2sExecutorWS",
		Method:      http.MethodGet,
		Path:        "/api/v1/addons/s2s/executor/ws",
		Summary:     "Executor WebSocket（S2S）",
		Description: "executor 建立双向 WebSocket，接收 a2a_request/task_kill，上报 stream_event/result/heartbeat",
		Tags:        []string{"S2S"},
	}, func(ctx context.Context, input *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				gc := humagin.Unwrap(hctx)
				executorToken := global.PRISM_VP.GetString("nucleagent.executor-token")
				ServeWS(gc.Writer, gc.Request, executorToken, handler)
			},
		}, nil
	})
	global.PRISM_LOG.Info("Executor-reg plugin routes registered")
}

func (p *ExecutorRegPlugin) Models() []interface{} { return nil }

// registerHTTPRegister 注册 POST /api/v1/addons/s2s/executor/register。
func registerHTTPRegister(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "s2sExecutorRegister",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/s2s/executor/register",
		Summary:     "Executor 注册（S2S）",
		Description: "executor 持 RegistrationKey/ExecutorToken 注册，返回 wsUrl",
		Tags:        []string{"S2S"},
	}, func(ctx context.Context, input *RegisterInput) (*RegisterOutput, error) {
		// 校验 X-Executor-Token（gin 中间件未覆盖 S2S，这里手动校验）。
		executorToken := global.PRISM_VP.GetString("nucleagent.executor-token")
		if executorToken != "" && input.XExecutorToken != executorToken {
			return nil, huma.NewError(http.StatusUnauthorized, "invalid executor token")
		}

		// 构造 wsUrl（从服务端可信配置派生，**不**信任客户端 Host 头，防注入）。
		wsURL := buildWSURLFromConfig()
		reg := RegisteredDevice{
			DeviceID:     input.Body.DeviceID,
			InstanceID:   input.Body.InstanceID,
			WSURL:        wsURL,
			RegisteredAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		global.PRISM_LOG.Info("executor registered",
			zap.String("device", reg.DeviceID),
			zap.String("instance", reg.InstanceID),
			zap.Strings("capabilities", input.Body.Capabilities))

		resp := &RegisterOutput{}
		resp.Body.Code = 0
		resp.Body.Message = "registered"
		resp.Body.Data = &reg
		return resp, nil
	})
}

// ---- S2S LLM key 签发（executor 服务级长效 key）----

// LLMKeyInput S2S LLM key 请求（X-Executor-Token 鉴权）。
type LLMKeyInput struct {
	XExecutorToken string `header:"X-Executor-Token" doc:"S2S 共享令牌"`
	Body           LLMKeyRequest
}

// LLMKeyRequest executor 请求服务级长效 LLM proxy key 的 body。
type LLMKeyRequest struct {
	ProviderID uint   `json:"providerId" required:"true"` // DB 里的 provider id
	Model      string `json:"model" required:"true"`      // 模型名（如 glm-5.2）
}

// LLMKeyOutput S2S LLM key 响应。
type LLMKeyOutput struct {
	Body struct {
		Code    int             `json:"code" example:"0"`
		Message string          `json:"message" example:"ok"`
		Data    *LLMKeyResponse `json:"data"`
	} `json:"body"`
}

// LLMKeyResponse 签发出的服务级 key + proxy 地址。
type LLMKeyResponse struct {
	Key          string `json:"key"`          // llmk_ 前缀临时 key（长效，proxy 滑动续期）
	ProxyBaseURL string `json:"proxyBaseUrl"` // LLM proxy base（executor 拼 /v1/chat/completions）
	Model        string `json:"model"`
	ExpiresIn    int    `json:"expiresIn"` // 秒
}

// executorServiceSession executor 服务级 key 的固定 sessionId（跨对话复用，TTL 滑动）。
const executorServiceSession = "nucleagent-executor"

// registerHTTPLLMKey 注册 POST /api/v1/addons/s2s/executor/llm-key。
// executor 启动时调一次，拿一个长效 key 写进 hermes managed config，常驻 hermes 缓存复用。
func registerHTTPLLMKey(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "s2sExecutorLLMKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/addons/s2s/executor/llm-key",
		Summary:     "Executor LLM key（S2S）",
		Description: "executor 持 ExecutorToken 换取服务级长效 LLM proxy key（hermes 常驻缓存用）",
		Tags:        []string{"S2S"},
	}, func(ctx context.Context, input *LLMKeyInput) (*LLMKeyOutput, error) {
		executorToken := global.PRISM_VP.GetString("nucleagent.executor-token")
		if executorToken != "" && input.XExecutorToken != executorToken {
			return nil, huma.NewError(http.StatusUnauthorized, "invalid executor token")
		}
		// 签发/复用 session 级长效 key（Redis 持久化时跨 core 重启有效）。
		tk := llmproxy.Default.GetOrIssueForSession(executorServiceSession, 0, input.Body.ProviderID, input.Body.Model)
		// proxy base = core public-url + /api/llm-proxy/v1
		base := global.PRISM_VP.GetString("nucleagent.public-url")
		if base == "" {
			addr := global.PRISM_CONFIG.System.Addr
			if addr == 0 {
				addr = 26680
			}
			base = "http://localhost:" + strconv.Itoa(addr)
		}
		resp := &LLMKeyOutput{}
		resp.Body.Code = 0
		resp.Body.Message = "ok"
		resp.Body.Data = &LLMKeyResponse{
			Key:          tk.Key,
			ProxyBaseURL: strings.TrimRight(base, "/") + "/api/llm-proxy/v1",
			Model:        tk.Model,
			ExpiresIn:    int(time.Until(tk.ExpiresAt).Seconds()),
		}
		return resp, nil
	})
}

// buildWSURLFromConfig 从服务端可信配置构造 wsUrl（**不**信任客户端 Host 头，防注入）。
//
// 优先 nucleagent.public-url（如 https://core.example.com）；
// 未配置时回退到监听端口 localhost:<system.addr>（仅本地 dev 用）。
func buildWSURLFromConfig() string {
	publicURL := global.PRISM_VP.GetString("nucleagent.public-url")
	base := publicURL
	if base == "" {
		addr := global.PRISM_CONFIG.System.Addr
		if addr == 0 {
			addr = 26680
		}
		base = "http://localhost:" + strconv.Itoa(addr)
	}
	scheme := "ws"
	if strings.HasPrefix(base, "https://") {
		scheme = "wss"
		base = strings.TrimPrefix(base, "https://")
	} else {
		base = strings.TrimPrefix(base, "http://")
	}
	return scheme + "://" + base + "/api/v1/addons/s2s/executor/ws"
}

// RegisterInput 注册请求输入（S2S 头直接挂在 input 上，huma 才能绑定）。
type RegisterInput struct {
	XExecutorToken string `header:"X-Executor-Token" doc:"S2S 共享令牌"`
	Body           RegisterRequest
}

// RegisterRequest 注册请求体。
type RegisterRequest struct {
	DeviceID     string   `json:"deviceId" required:"true"`
	InstanceID   string   `json:"instanceId,omitempty"`
	DeviceName   string   `json:"deviceName,omitempty"`
	BackendType  string   `json:"backendType,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ExecutorRegisterOutputBody 注册响应体（命名 struct，避免与 auth 的 RegisterOutput 重名冲突）。
type ExecutorRegisterOutputBody struct {
	Code    int               `json:"code" example:"0"`
	Message string            `json:"message" example:"registered"`
	Data    *RegisteredDevice `json:"data"`
}

// RegisterOutput 注册响应。
type RegisterOutput struct {
	Body ExecutorRegisterOutputBody
}
