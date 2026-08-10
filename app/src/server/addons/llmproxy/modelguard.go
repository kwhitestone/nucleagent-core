// Package llmproxy 的模型白名单校验。
//
// 存在的理由：llmproxy 是纯凭据注入反代 —— 它把请求体原样转发给上游，**从不改写
// 其中的 model 字段**。也就是说实际执行并计费的模型完全由调用方（hermes）决定，
// 而 TempLLMKey.Model 只被用来写日志。没有白名单时，任何持有临时 key 的调用方
// 都能用我们的 API key 请求该 provider 的任意模型。
//
// 校验放在 proxy 层而不只在业务层：业务层（create/patch/follow-up）能挡住 UI 选错，
// 但挡不住直接打 proxy 端点的调用方，而后者才是真正花钱的那一跳。
package llmproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nucleagent/nucleagent-shared/model"

	"whitestone.top/prism-fusion/global"
)

// restoreBody 把已读出的请求体放回 req，供后续 ReverseProxy 转发。
//
// 三个字段必须一起设置，少一个就会出问题：
//   - Body：真正被读走的流；
//   - ContentLength：Transport 按它决定写多少字节，不同步会截断或挂住；
//   - GetBody：Transport 在重定向/重试时靠它重新拿 body，缺了会静默丢请求体。
func restoreBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

// ErrModelNotAllowed 请求的模型不在 provider 的 config.models 白名单内。
var ErrModelNotAllowed = errors.New("模型不在该提供商的可用列表内")

// ProviderModels 返回 provider 配置的模型白名单。
//
// 返回空切片有两种含义（调用方无需区分）：provider 没配 models，或配了空列表。
// 两者都按「未启用白名单」处理 —— 见 ValidateModel 的说明。
func ProviderModels(providerID uint) ([]string, error) {
	if global.PRISM_DB == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}
	var p model.Provider
	if err := global.PRISM_DB.First(&p, providerID).Error; err != nil {
		return nil, fmt.Errorf("provider %d 不存在: %w", providerID, err)
	}
	var cfg ProviderConfig
	if len(p.Config) > 0 {
		if err := json.Unmarshal(p.Config, &cfg); err != nil {
			return nil, fmt.Errorf("provider %d config 解析失败: %w", providerID, err)
		}
	}
	return cfg.Models, nil
}

// ValidateModel 校验 model 是否在 provider 的白名单内。
//
// 空 model 视为「用兜底默认」，放行 —— 上游会用它自己的默认模型，这是既有行为。
//
// **白名单为空时放行**：这是有意的向后兼容。现存 provider 未必配了 models，
// 一上线就强校验会让所有对话立刻不可用。只有显式配了清单的 provider 才强校验。
func ValidateModel(providerID uint, modelName string) error {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}
	models, err := ProviderModels(providerID)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	for _, m := range models {
		if strings.EqualFold(strings.TrimSpace(m), modelName) {
			return nil
		}
	}
	// 错误信息带上可用清单：调用方（和 UI）据此能自己纠正，不用去翻库。
	return fmt.Errorf("%w: %s（可用：%s）", ErrModelNotAllowed, modelName, strings.Join(models, ", "))
}

// modelFromRequestBody 从 LLM 请求体里取 model 字段。
//
// 取不到（无 model 键、body 为空、非 JSON）一律返回空串而不报错：embeddings 等
// 端点的请求体形态不同，不该因为解不出 model 就拒掉整个请求。校验由调用方按
// 「空 = 放行」处理。
func modelFromRequestBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Model)
}
