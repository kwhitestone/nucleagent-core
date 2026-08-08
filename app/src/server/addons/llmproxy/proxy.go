package llmproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/nucleagent/nucleagent-shared/model"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"whitestone.top/prism-fusion/global"
)

// RegisterRoutes 注册 LLM Proxy 路由。
//
// Proxy 是反向代理，用 huma StreamResponse + humagin.Unwrap 拿 gin context 做透传
// （透传请求体 + 流式响应）。路由前缀 /api/llm-proxy，**不**在 /api/v1/addons 下，
// 故不受 JWT 中间件作用域影响（用 x-llm-proxy-key 自鉴权）。
func RegisterRoutes(api huma.API) {
	registerProxyRoute(api, "/api/llm-proxy/v1/chat/completions")
	registerProxyRoute(api, "/api/llm-proxy/v1/completions")
	registerProxyRoute(api, "/api/llm-proxy/v1/embeddings")
	global.PRISM_LOG.Info("LLM proxy routes registered")
}

// registerProxyRoute 注册单个 proxy 路径（POST）。
func registerProxyRoute(api huma.API, path string) {
	huma.Register(api, huma.Operation{
		OperationID: "llmProxy" + strings.ReplaceAll(strings.Trim(path, "/"), "/", "_"),
		Method:      http.MethodPost,
		Path:        path,
		Summary:     "LLM Proxy 反向代理",
		Tags:        []string{"LLMProxy"},
	}, func(ctx context.Context, input *struct{}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				gc := humagin.Unwrap(hctx)
				handleProxy(gc)
			},
		}, nil
	})
}

// handleProxy 反向代理核心。
func handleProxy(c *gin.Context) {
	start := time.Now()

	// 1. 验签 x-llm-proxy-key（兼容 Authorization: Bearer —— OpenAI 兼容客户端
	//    如 hermes custom provider 把 key 放 Bearer，而非自定义头）。
	tempKey := c.GetHeader(llm.KeyHeader)
	if tempKey == "" {
		if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			tempKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	tk, rp, err := resolveByTempKey(tempKey)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "invalid or expired llm proxy key",
		})
		return
	}

	// 2. 解析目标 URL。
	target, err := url.Parse(rp.BaseURL)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "provider base_url invalid",
		})
		return
	}

	// 3. 构造反向代理。
	proxy := httputil.NewSingleHostReverseProxy(target)
	// 让出站请求走 HTTP(S)_PROXY 环境变量（部分 LLM 端点需经正向代理才可达）。
	proxy.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment}

	// 自定义 Director：去掉 /api/llm-proxy 前缀，注入真实鉴权头。
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		// 先在原始 path 上去掉 /api/llm-proxy 前缀，再交给默认 Director
		// 与 target.Path 拼接（否则拼接后前缀在中间，TrimPrefix 够不着）。
		stripped := strings.TrimPrefix(req.URL.Path, "/api/llm-proxy")
		// 进一步去掉 OpenAI 标准的 /v1 段，让 provider base_url 自己决定版本路径
		// （如 GLM 的 base_url 含 /v4，hermes/openai 客户端带 /v1 会拼成 /v4/v1/…）。
		stripped = strings.TrimPrefix(stripped, "/v1")
		if stripped == "" {
			stripped = "/"
		}
		req.URL.Path = stripped
		req.URL.RawPath = ""
		originalDirector(req)
		// 关键：ReverseProxy 默认保留原始 Host（localhost:26680），但部分 LLM 端点
		// （如 GLM 阿里云 WAF）按 Host 做虚拟主机/风控，Host 不匹配直接 410。
		// 覆盖为目标 Host。
		req.Host = target.Host
		// 清理客户端的临时鉴权头（x-llm-proxy-key + hermes 的 Bearer 临时 key），
		// 注入真实 provider key（按 AuthScheme）。
		req.Header.Del(llm.KeyHeader)
		req.Header.Del("Authorization")
		applyAuth(req, rp)
	}

	// 4. 流式记录响应用于 CallLog：用 TeeReader 边透传边捕获前缀预览，
	//    **不**用 io.ReadAll 缓冲整个 body（否则 SSE 流式被破坏 + >1MB 响应被截断）。
	var respBuf responseRecorder
	proxy.ModifyResponse = func(resp *http.Response) error {
		respBuf.statusCode = resp.StatusCode
		respBuf.contentType = resp.Header.Get("Content-Type")
		// 用 limitWriter 把响应流的前 llmProxyLogMaxBytes 字节 tee 到缓冲（超过丢弃），
		// 完整 body 仍由 ReverseProxy 流式透传给客户端。
		lw := newLimitWriter(llmProxyLogMaxBytes)
		respBuf.capture = lw
		resp.Body = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.TeeReader(resp.Body, lw),
			Closer: resp.Body,
		}
		return nil
	}

	// 5. 执行代理（流式透传）。
	proxy.ServeHTTP(c.Writer, c.Request)

	// 6. 写 CallLog（异步，不阻塞响应）。
	go writeCallLog(tk, rp, start, c.Request, respBuf)
}

// limitWriter 把写入的前 max 字节存入缓冲，超过后静默丢弃。
// 用于 tee 捕获响应体前缀做日志，不缓冲整个 body（避免破坏流式 + 截断）。
type limitWriter struct {
	max   int
	buf   []byte
	drop  bool
}

func newLimitWriter(max int) *limitWriter { return &limitWriter{max: max} }

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.drop {
		return len(p), nil // 已满，丢弃但报告写入成功（让 TeeReader 继续）。
	}
	remain := w.max - len(w.buf)
	if remain <= 0 {
		w.drop = true
		return len(p), nil
	}
	if len(p) <= remain {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	w.buf = append(w.buf, p[:remain]...)
	w.drop = true
	return len(p), nil
}

func (w *limitWriter) Bytes() []byte { return w.buf }

// applyAuth 按 AuthScheme 注入真实 API key。
func applyAuth(req *http.Request, rp llm.ResolvedProvider) {
	switch rp.AuthScheme {
	case "api_key":
		// 自定义 header（如 x-api-key），v1 用默认 X-API-Key。
		req.Header.Set("X-API-Key", rp.APIKey)
	default: // bearer
		req.Header.Set("Authorization", "Bearer "+rp.APIKey)
	}
}

// responseRecorder 记录代理响应的状态码与捕获的前缀 body，用于 CallLog。
type responseRecorder struct {
	statusCode  int
	contentType string
	capture     *limitWriter // tee 捕获的响应体前缀
}

// llmProxyLogMaxBytes CallLog 记录的响应体上限。
const llmProxyLogMaxBytes = 1 << 20 // 1MB

// writeCallLog 异步写 LLM 调用日志。
func writeCallLog(tk llm.TempLLMKey, rp llm.ResolvedProvider, start time.Time, req *http.Request, rec responseRecorder) {
	if global.PRISM_DB == nil {
		return
	}
	// 读取请求体（已被 proxy 消费，这里只记录元数据 + 摘要）。
	inputPreview := fmt.Sprintf("%s %s", req.Method, req.URL.Path)
	errMsg := ""
	if rec.statusCode >= 400 {
		errMsg = fmt.Sprintf("http %d", rec.statusCode)
	}
	// 从 limitWriter 取捕获的前缀（流式响应只截前缀做日志，不缓冲全部）。
	var outputPreview string
	if rec.capture != nil {
		outputPreview = string(rec.capture.Bytes())
	}

	log := &model.CallLog{
		ConversationID: tk.ConversationID,
		StepID:         "", // 由后续上下文注入（v1 占位）
		CallType:       llm.CallTypeLLM,
		Model:          rp.Model,
		Input:          inputPreview,
		Output:         outputPreview,
		Meta: model.MustNewJSON(map[string]any{
			"latencyMs":   time.Since(start).Milliseconds(),
			"status":      rec.statusCode,
			"contentType": rec.contentType,
			"error":       errMsg,
		}),
	}
	if err := global.PRISM_DB.Create(log).Error; err != nil {
		global.PRISM_LOG.Warn("llmproxy: write call log failed", zap.Error(err))
	}
}
