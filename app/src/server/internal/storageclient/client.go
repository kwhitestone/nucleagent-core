// Package storageclient 访问 nucleagent-storage 的 S2S 客户端。
//
// core 用它做两件事，都**不涉及文件字节**：
//   - Stat：校验前端回传的 fileId 真实存在、已 active、且属于本命名空间；
//   - PresignDownload：为 executor 签一条 CDN 直链，executor 自己去拉。
//
// 鉴权：storage 与 core 共享 JWT signing-key（见 storage/config.yaml 的 jwt 段），
// 故这里用框架的 JwtService 自签一个服务身份 token，无需额外的 S2S 密钥。
package storageclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	authservice "github.com/kwhitestone/prism-fusion/addons/auth/service"
)

// ErrNotFound 文件不存在（storage 返回 404，或命名空间不符）。
var ErrNotFound = errors.New("附件不存在")

// ErrNotActive 文件存在但未完成上传（status != active）。
var ErrNotActive = errors.New("附件尚未完成上传")

// serviceUserID 自签 token 用的用户 ID。
//
// 0 表示「服务自身」而非某个真实用户：storage 只用 user_id 做 created_by 审计，
// 权限判定靠 namespace，故不需要冒用真实用户身份。
const serviceUserID = 0

// serviceName core 在 storage 侧的调用方标识（写进 X-Service-Name 供审计）。
const serviceName = "nucleagent-core"

// statusActive storage 侧「上传已完成」的状态值。
const statusActive = "active"

// Client storage 的 HTTP 客户端。
type Client struct {
	baseURL   string
	namespace string
	http      *http.Client
	jwt       authservice.JwtService
}

// New 构造客户端。
//
// baseURL 为空时返回 nil —— 调用方据此判断「未配置 storage」并降级（不带附件的
// 对话必须照常可用），而不是让每次调用都失败。
func New(baseURL, namespace string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if namespace == "" {
		namespace = "core"
	}
	return &Client{
		baseURL:   baseURL,
		namespace: namespace,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				// 显式关掉代理。宿主机常设 HTTP_PROXY=localhost:7897，Go 默认走
				// ProxyFromEnvironment，会把 localhost 的服务间调用发到代理端口
				// → 502。dev.sh 里 unset 代理是同一个坑的另一种兜法，但库不能
				// 依赖调用方的环境卫生。
				Proxy:               nil,
				DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				MaxIdleConnsPerHost: 4,
			},
		},
	}
}

// Namespace 返回客户端使用的命名空间。
func (c *Client) Namespace() string { return c.namespace }

// File storage 返回的文件元数据（只取 core 需要的字段）。
type File struct {
	FileID    string `json:"fileId"`
	Namespace string `json:"namespace"`
	OrigName  string `json:"origName"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType"`
	SHA256    string `json:"sha256"`
	Status    string `json:"status"`
}

// DownloadResult storage 签发的下载信息。
type DownloadResult struct {
	URL       string `json:"url"`
	ExpiresIn int    `json:"expiresIn"`
	Name      string `json:"name"`
	MimeType  string `json:"mimeType"`
	Size      int64  `json:"size"`
}

// envelope storage 的 {code, message, data} 响应信封。
type envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// Stat 查询文件元数据，并校验它可用于本次执行。
//
// 必须做这一步：fileId 来自前端，直接信任等于允许调用方引用任意文件。
// storage 侧虽有跨命名空间保护，但那只挡住「别的服务」，挡不住同命名空间内
// 伪造的 ID —— 存在性与 active 状态必须由 core 自己确认。
func (c *Client) Stat(ctx context.Context, fileID string) (*File, error) {
	if fileID == "" {
		return nil, fmt.Errorf("fileId 不能为空")
	}
	var env envelope[*File]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/files/"+url.PathEscape(fileID), &env); err != nil {
		return nil, err
	}
	if env.Data == nil {
		return nil, ErrNotFound
	}
	// 命名空间不符按「不存在」对外表达，不泄露「该 ID 属于别人」。
	if env.Data.Namespace != c.namespace {
		return nil, ErrNotFound
	}
	if env.Data.Status != statusActive {
		return nil, ErrNotActive
	}
	return env.Data, nil
}

// PresignDownload 为一个文件签发下载直链（CDN），供 executor 拉取字节。
func (c *Client) PresignDownload(ctx context.Context, fileID string) (*DownloadResult, error) {
	if fileID == "" {
		return nil, fmt.Errorf("fileId 不能为空")
	}
	var env envelope[*DownloadResult]
	path := "/api/v1/files/" + url.PathEscape(fileID) + "/download"
	if err := c.doJSON(ctx, http.MethodGet, path, &env); err != nil {
		return nil, err
	}
	if env.Data == nil || env.Data.URL == "" {
		return nil, fmt.Errorf("storage 未返回下载地址")
	}
	return env.Data, nil
}

// doJSON 发一次请求并解出信封。
func (c *Client) doJSON(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	token, err := c.jwt.GenerateToken(serviceUserID, serviceName, 0)
	if err != nil {
		return fmt.Errorf("签发 storage 访问令牌失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Namespace", c.namespace)
	req.Header.Set("X-Service-Name", serviceName)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 storage 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 不带上游响应体：它可能含签名 URL 等敏感串。
		return fmt.Errorf("storage 返回 %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析 storage 响应失败: %w", err)
	}
	return nil
}
