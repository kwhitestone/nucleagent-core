// Package llmproxy core 的 LLM 代理：签发 TempLLMKey + 反向代理到真实 LLM endpoint。
//
// 参考 agentia-engine/src/service/llmproxy/store.go 的设计：
//   - 内存 map 始终是兜底（无 Redis 时行为完整）
//   - Redis 是可选增强：double-write 内存+Redis，Lookup 时 Redis 优先（miss 回退内存）
//   - GetOrIssueForSession：按 (sessionId,userId,model) 复用 key + TTL 滑动续期
//   - RefreshTTL：proxy 每次成功请求续期，活跃会话永不过期
//
// 这样 core 重启时：有 Redis → 内存重建（Redis miss 回退）/跨实例可见；
// 无 Redis → 内存丢失（dev 可接受，重启后新对话自然拿新 key）。
package llmproxy

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	tempKeyTTL          = 2 * time.Hour // 对齐 agentia tempKeyTTL
	redisKeyPrefix      = "nucleagent:llmkey:"  // nucleagent:llmkey:<key> → TempLLMKey JSON
	redisSessionPrefix  = "nucleagent:llmsess:" // nucleagent:llmsess:<sessionID> → set of keys
	redisOpTimeout      = 200 * time.Millisecond
)

// KeyStore TempLLMKey 存储：内存 map 兜底 + 可选 Redis 增强。
//
// 内存 map 始终在；Redis（client 非 nil 时）做 double-write，Lookup 优先读 Redis
//（覆盖跨实例/重启场景），miss 回退内存。与 agentia store.go 同构。
type KeyStore struct {
	mu     sync.RWMutex
	keys   map[string]llm.TempLLMKey // key 明文 -> 元数据
	redis  *redis.Client             // 可选；nil 时纯内存
}

// NewKeyStore 构造空 store（纯内存）。
func NewKeyStore() *KeyStore { return &KeyStore{keys: make(map[string]llm.TempLLMKey)} }

// Default 全局默认 store（main 启动时用 SetRedis 注入 Redis）。
var Default = NewKeyStore()

// SetRedis 注入 Redis client（nil = 纯内存模式）。返回是否启用 Redis。
func (s *KeyStore) SetRedis(client *redis.Client) bool {
	s.redis = client
	return client != nil
}

// InitDefault 用给定 addr 探测并初始化 Default 的 Redis。addr 空或不可达则纯内存。
// 返回是否启用了 Redis。
func InitDefault(addr string) bool {
	if addr == "" {
		return Default.SetRedis(nil)
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 2 * time.Second})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return Default.SetRedis(nil)
	}
	return Default.SetRedis(client)
}

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
	s.saveKey(k)
	return k
}

// GetOrIssueForSession 按 (sessionID, userID, model) 复用 session 级 key（滑动续期），
// 不存在则签发。供 executor 服务级长效 key 用（hermes 常驻缓存一次，TTL 滑动永不过期）。
//
// sessionId 是稳定的逻辑会话标识（如 "executor-hermes"），不绑定单次对话。
// providerID/model 决定 key 解析到哪个 provider。
func (s *KeyStore) GetOrIssueForSession(sessionID string, userID, providerID uint, model string) llm.TempLLMKey {
	// 1. Redis 优先找已有 key
	if tk, ok := s.redisFindKeyBySession(sessionID, userID, model); ok {
		tk.ProviderID = providerID
		tk.ExpiresAt = time.Now().Add(tempKeyTTL)
		s.saveKey(tk)
		return tk
	}
	// 2. 内存找
	s.mu.Lock()
	now := time.Now()
	for _, v := range s.keys {
		if v.SessionID == sessionID && v.UserID == userID && v.Model == model && v.ExpiresAt.After(now) {
			v.ProviderID = providerID
			v.ExpiresAt = now.Add(tempKeyTTL)
			s.keys[v.Key] = v
			s.mu.Unlock()
			s.redisStoreKey(v)
			return v
		}
	}
	// 3. 签发新 key（session 级：ConversationID=0，SessionID 非空）
	k := llm.TempLLMKey{
		Key:       llm.KeyPrefix + uuid.NewString(),
		SessionID: sessionID,
		UserID:    userID,
		ProviderID: providerID,
		Model:     model,
		ExpiresAt: now.Add(tempKeyTTL),
	}
	s.keys[k.Key] = k
	s.mu.Unlock()
	s.redisStoreKey(k)
	return k
}

// RefreshTTL 把 key 的过期时间滑动到完整 TTL（proxy 每次成功请求调用）。
// 活跃会话永不中断。未知 key no-op。
func (s *KeyStore) RefreshTTL(plain string) {
	s.mu.Lock()
	k, ok := s.keys[plain]
	if ok {
		k.ExpiresAt = time.Now().Add(tempKeyTTL)
		s.keys[plain] = k
	}
	s.mu.Unlock()
	if ok {
		s.redisStoreKey(k)
		return
	}
	// 可能只在 Redis 里（别的实例签发的）：领养进内存
	if rk, ok := s.redisLookupKey(plain); ok {
		rk.ExpiresAt = time.Now().Add(tempKeyTTL)
		s.mu.Lock()
		s.keys[plain] = rk
		s.mu.Unlock()
		s.redisStoreKey(rk)
	}
}

// Lookup 校验 key 明文，返回元数据。过期/不存在返回 false。
// Redis 优先（跨实例/重启），miss 回退内存。
func (s *KeyStore) Lookup(plain string) (llm.TempLLMKey, bool) {
	if k, ok := s.redisLookupKey(plain); ok {
		return k, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.keys[plain]
	if !ok || time.Now().After(k.ExpiresAt) {
		return llm.TempLLMKey{}, false
	}
	return k, true
}

// Revoke 撤销指定 key。
func (s *KeyStore) Revoke(plain string) {
	s.mu.Lock()
	delete(s.keys, plain)
	s.mu.Unlock()
	s.redisDeleteKey(plain)
}

// RevokeByConversation 撤销某 conversation 下所有 key。
// session 级 key（ConversationID=0）不被撤销。
func (s *KeyStore) RevokeByConversation(convID uint) {
	if convID == 0 {
		return
	}
	s.mu.Lock()
	for plain, k := range s.keys {
		if k.ConversationID == convID {
			delete(s.keys, plain)
		}
	}
	s.mu.Unlock()
	// session 级 key 不按 conversation 索引，Redis 侧无需清理（TTL 自过期）
}

// saveKey 写内存 + Redis。
func (s *KeyStore) saveKey(k llm.TempLLMKey) {
	s.mu.Lock()
	s.keys[k.Key] = k
	s.mu.Unlock()
	s.redisStoreKey(k)
}

// StartCleanupLoop 定期清理过期 key（仅内存；Redis 靠 TTL）。
func (s *KeyStore) StartCleanupLoop(ctx context.Context) {
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.cleanupExpired()
			}
		}
	}()
}

func (s *KeyStore) cleanupExpired() int {
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

// String 标识当前模式（日志用）。
func (s *KeyStore) String() string {
	if s.redis != nil {
		return "redis"
	}
	return "memory"
}

// --- Redis helpers（client 为 nil 时全部 no-op，退化为纯内存）---

func (s *KeyStore) redisStoreKey(k llm.TempLLMKey) {
	if s.redis == nil {
		return
	}
	data, err := json.Marshal(k)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	pipe := s.redis.TxPipeline()
	pipe.Set(ctx, redisKeyPrefix+k.Key, data, tempKeyTTL)
	if k.SessionID != "" {
		pipe.SAdd(ctx, redisSessionPrefix+k.SessionID, k.Key)
		pipe.Expire(ctx, redisSessionPrefix+k.SessionID, tempKeyTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		// Redis 故障不影响正确性（内存仍可用），仅 debug 日志。
	}
}

func (s *KeyStore) redisLookupKey(plain string) (llm.TempLLMKey, bool) {
	if s.redis == nil {
		return llm.TempLLMKey{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	data, err := s.redis.Get(ctx, redisKeyPrefix+plain).Bytes()
	if err != nil || len(data) == 0 {
		return llm.TempLLMKey{}, false
	}
	var k llm.TempLLMKey
	if json.Unmarshal(data, &k) != nil {
		return llm.TempLLMKey{}, false
	}
	if time.Now().After(k.ExpiresAt) {
		return llm.TempLLMKey{}, false
	}
	return k, true
}

func (s *KeyStore) redisFindKeyBySession(sessionID string, userID uint, model string) (llm.TempLLMKey, bool) {
	if s.redis == nil || sessionID == "" {
		return llm.TempLLMKey{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	keys, err := s.redis.SMembers(ctx, redisSessionPrefix+sessionID).Result()
	if err != nil {
		return llm.TempLLMKey{}, false
	}
	for _, k := range keys {
		if tk, ok := s.redisLookupKey(k); ok && tk.UserID == userID && tk.Model == model {
			return tk, true
		}
	}
	return llm.TempLLMKey{}, false
}

func (s *KeyStore) redisDeleteKey(plain string) {
	if s.redis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	s.redis.Del(ctx, redisKeyPrefix+plain)
}
