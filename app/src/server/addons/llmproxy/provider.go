package llmproxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/nucleagent/nucleagent-shared/model"

	"whitestone.top/prism-fusion/global"

	"gorm.io/gorm"
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

// resolveByTempKey 通过 TempLLMKey 解析 Provider（Proxy 端点验签入口）。
func resolveByTempKey(tempKey string) (llm.TempLLMKey, llm.ResolvedProvider, error) {
	tk, ok := Default.Lookup(tempKey)
	if !ok {
		return llm.TempLLMKey{}, llm.ResolvedProvider{}, gorm.ErrRecordNotFound
	}
	rp, err := ResolveProvider(tk.ProviderID)
	if err != nil {
		return tk, llm.ResolvedProvider{}, err
	}
	rp.Model = tk.Model
	return tk, rp, nil
}
