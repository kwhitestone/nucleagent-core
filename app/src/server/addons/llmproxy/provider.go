package llmproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/nucleagent/nucleagent-shared/model"

	"whitestone.top/prism-fusion/global"
)

// ProviderConfig Provider.config JSON 的结构。
type ProviderConfig struct {
	BaseURL    string   `json:"baseUrl"`
	APIFormat  string   `json:"apiFormat"`  // openai / anthropic
	AuthScheme string   `json:"authScheme"` // bearer / api_key
	Models     []string `json:"models"`
}

// ResolveProvider 根据 providerID 从 DB 加载 Provider，解密 API key，返回解析后的信息。
//
// 返回的 ResolvedProvider.APIKey 仅在 Proxy 进程内存中短暂持有用于转发，不落日志。
func ResolveProvider(providerID uint) (llm.ResolvedProvider, error) {
	var p model.Provider
	if err := global.PRISM_DB.First(&p, providerID).Error; err != nil {
		return llm.ResolvedProvider{}, fmt.Errorf("llmproxy: provider %d not found: %w", providerID, err)
	}
	if !p.IsActive {
		return llm.ResolvedProvider{}, fmt.Errorf("llmproxy: provider %d inactive", providerID)
	}

	var cfg ProviderConfig
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		return llm.ResolvedProvider{}, fmt.Errorf("llmproxy: provider %d config invalid: %w", providerID, err)
	}

	// SSRF 防护：baseUrl 必须是 http/https，且非空。
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return llm.ResolvedProvider{}, fmt.Errorf("llmproxy: provider %d has no base_url", providerID)
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return llm.ResolvedProvider{}, fmt.Errorf("llmproxy: provider %d base_url must be http/https", providerID)
	}
	cfg.BaseURL = baseURL

	apiKey, err := DecryptAPIKey(p.APIKey)
	if err != nil {
		return llm.ResolvedProvider{}, fmt.Errorf("llmproxy: provider %d api_key decrypt failed: %w", providerID, err)
	}

	return llm.ResolvedProvider{
		ProviderID: p.ID,
		BaseURL:    cfg.BaseURL,
		APIKey:     apiKey,
		APIFormat:  cfg.APIFormat,
		AuthScheme: cfg.AuthScheme,
	}, nil
}

// ErrKeyNotFound 表示 TempLLMKey 查不到或已过期（真正的「鉴权失败」）。
//
// 与之相对，ResolveProvider 的错误是**服务端配置问题**（provider 不存在/停用/
// base_url 非法/MASTER_KEY 未设导致 api_key 解密失败），不是调用方的凭证问题。
// 两者曾被 proxy 合并成同一句 401 "invalid or expired llm proxy key"，
// 导致 MASTER_KEY 缺失被误读为 key 过期，排查方向被带偏。务必保持区分。
var ErrKeyNotFound = errors.New("llmproxy: temp key not found or expired")

// resolveByTempKey 通过 TempLLMKey 解析 Provider（Proxy 端点验签入口）。
//
// 返回的 error 分两类，调用方据此区分 401 与 500：
//   - ErrKeyNotFound：凭证无效 → 401
//   - 其他：provider 解析/解密失败 → 500
func resolveByTempKey(tempKey string) (llm.TempLLMKey, llm.ResolvedProvider, error) {
	tk, ok := Default.Lookup(tempKey)
	if !ok {
		return llm.TempLLMKey{}, llm.ResolvedProvider{}, ErrKeyNotFound
	}
	rp, err := ResolveProvider(tk.ProviderID)
	if err != nil {
		return tk, llm.ResolvedProvider{}, err
	}
	rp.Model = tk.Model
	return tk, rp, nil
}
