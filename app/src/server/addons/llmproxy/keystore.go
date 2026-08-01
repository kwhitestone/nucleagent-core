// Package llmproxy core 的 LLM 代理：签发 TempLLMKey + 反向代理到真实 LLM endpoint。
//
// 参考 agentia-engine/src/service/llm_proxy.go：
//   - executor 永不直接持有 API key（硬约束 D1）
//   - core 为每个 conversation 签发 TempLLMKey，executor 在请求头携带 x-llm-proxy-key
//   - core Proxy 端点验签 -> 解析 Provider + 解密 API key -> 反向代理到真实 LLM
//   - 透传流式响应 -> 写 CallLog
//
// v1 只支持 OpenAI 兼容格式（/v1/chat/completions）。
package llmproxy

import (
	"context"
	"sync"
	"time"

	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/google/uuid"
)

// KeyStore TempLLMKey 内存存储（进程内）。
//
// v1 进程内内存即可；多实例后置改 Redis。key 明文只此一处保存，
// Proxy 验签时反查。
type KeyStore struct {
	mu   sync.RWMutex
	keys map[string]llm.TempLLMKey // key 明文 -> 元数据
}

// NewKeyStore 构造空 store。
func NewKeyStore() *KeyStore {
	return &KeyStore{keys: make(map[string]llm.TempLLMKey)}
}

// Default 全局默认 store（供 Proxy 端点 + conversation 签发共用）。
var Default = NewKeyStore()

// Issue 为 conversation 签发临时 key。
func (s *KeyStore) Issue(convID, userID, providerID uint, model string, ttl time.Duration) llm.TempLLMKey {
	k := llm.TempLLMKey{
		Key:            llm.KeyPrefix + uuid.NewString(),
		ConversationID: convID,
		UserID:         userID,
		ProviderID:     providerID,
		Model:          model,
		ExpiresAt:      time.Now().Add(ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[k.Key] = k
	return k
}

// Lookup 校验 key 明文，返回元数据。过期/不存在返回 false。
func (s *KeyStore) Lookup(plain string) (llm.TempLLMKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.keys[plain]
	if !ok {
		return llm.TempLLMKey{}, false
	}
	if time.Now().After(k.ExpiresAt) {
		return llm.TempLLMKey{}, false
	}
	return k, true
}

// Revoke 撤销指定 key（conversation 结束时调用）。
func (s *KeyStore) Revoke(plain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, plain)
}

// RevokeByConversation 撤销某 conversation 下所有 key。
func (s *KeyStore) RevokeByConversation(convID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for plain, k := range s.keys {
		if k.ConversationID == convID {
			delete(s.keys, plain)
		}
	}
}

// CleanupExpired 清除已过期的 key（定期调用，避免内存累积）。
func (s *KeyStore) CleanupExpired() int {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for plain, k := range s.keys {
		if now.After(k.ExpiresAt) {
			delete(s.keys, plain)
			removed++
		}
	}
	return removed
}

// StartCleanupLoop 启动定期清理 goroutine（每 5 分钟）。
// 返回 stop func，传入 context 可取消。
func (s *KeyStore) StartCleanupLoop(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.CleanupExpired()
			}
		}
	}()
}
