package llmproxy

import (
	"os"
	"testing"
	"time"

	"github.com/nucleagent/nucleagent-shared/llm"
	"github.com/spf13/viper"

	"whitestone.top/prism-fusion/global"
)

// setMasterKey 设置测试用 MASTER_KEY（32 字节 hex）。
func setMasterKey(t *testing.T) {
	t.Helper()
	// 32 字节 -> hex 64 字符。
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	os.Setenv("MASTER_KEY", key)
	t.Cleanup(func() { os.Unsetenv("MASTER_KEY") })
}

// TestCryptoRoundTrip 验证 API key 加解密往返。
func TestCryptoRoundTrip(t *testing.T) {
	setMasterKey(t)
	plain := "sk-test-1234567890abcdef"
	enc, err := EncryptAPIKey(plain)
	if err != nil {
		t.Fatalf("EncryptAPIKey: %v", err)
	}
	if enc == plain {
		t.Error("ciphertext should differ from plaintext")
	}
	got, err := DecryptAPIKey(enc)
	if err != nil {
		t.Fatalf("DecryptAPIKey: %v", err)
	}
	if got != plain {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plain)
	}
}

// TestCryptoWrongKey 验证错误密钥解密失败。
func TestCryptoWrongKey(t *testing.T) {
	setMasterKey(t)
	enc, _ := EncryptAPIKey("secret")
	// 换密钥。
	os.Setenv("MASTER_KEY", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
	_, err := DecryptAPIKey(enc)
	if err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

// TestCryptoNoMasterKey 验证无 MASTER_KEY 报错。
func TestCryptoNoMasterKey(t *testing.T) {
	os.Unsetenv("MASTER_KEY")
	_, err := EncryptAPIKey("x")
	if err == nil {
		t.Error("encrypt without MASTER_KEY should fail")
	}
}

// TestMasterKeyFromViper 验证主密钥优先从 viper（config.yaml 的
// nucleagent.master-key）读取——这是它接入统一配置体系后的主路径。
func TestMasterKeyFromViper(t *testing.T) {
	const viperKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	os.Unsetenv("MASTER_KEY") // 确保不是走 env 回退
	vp := viper.New()
	vp.Set("nucleagent.master-key", viperKey)
	global.PRISM_VP = vp
	t.Cleanup(func() { global.PRISM_VP = nil })

	if got := masterKeyRaw(); got != viperKey {
		t.Errorf("masterKeyRaw should read from viper: got %q", got)
	}
	if err := ValidateMasterKey(); err != nil {
		t.Errorf("ValidateMasterKey with valid viper key: %v", err)
	}
}

// TestMasterKeyUnexpandedPlaceholderFallsBackToEnv 验证 viper 里残留未展开的
// "${MASTER_KEY}" 字面量时不被当成密钥，而是回退到环境变量。
//
// 这是真实故障模式：config.yaml 写 '${MASTER_KEY}'，若 expandNucleagentEnv
// 漏掉该键，viper 里就是字面量——绝不能把它当 32 字节密钥用。
func TestMasterKeyUnexpandedPlaceholderFallsBackToEnv(t *testing.T) {
	const envKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	vp := viper.New()
	vp.Set("nucleagent.master-key", "${MASTER_KEY}") // 未展开
	global.PRISM_VP = vp
	os.Setenv("MASTER_KEY", envKey)
	t.Cleanup(func() {
		global.PRISM_VP = nil
		os.Unsetenv("MASTER_KEY")
	})

	if got := masterKeyRaw(); got != envKey {
		t.Errorf("unexpanded placeholder should fall back to env: got %q", got)
	}
}

// TestValidateMasterKeyRejectsBadInput 验证启动期自检能挡住缺失与错长度。
func TestValidateMasterKeyRejectsBadInput(t *testing.T) {
	global.PRISM_VP = nil

	os.Unsetenv("MASTER_KEY")
	if err := ValidateMasterKey(); err == nil {
		t.Error("missing MASTER_KEY should fail validation")
	}

	// 长度不足（非 32 字节、非 64 hex）。
	os.Setenv("MASTER_KEY", "tooshort")
	t.Cleanup(func() { os.Unsetenv("MASTER_KEY") })
	if err := ValidateMasterKey(); err == nil {
		t.Error("short MASTER_KEY should fail validation")
	}
}

// TestKeyStoreIssueLookupRevoke 验证 TempLLMKey 签发/校验/撤销。
func TestKeyStoreIssueLookupRevoke(t *testing.T) {
	s := NewKeyStore()
	tk := s.Issue(100, 200, 1, "gpt-4o-mini", 30*time.Minute)

	if tk.Key == "" || tk.ConversationID != 100 {
		t.Fatalf("issued key invalid: %+v", tk)
	}

	// Lookup 命中。
	got, ok := s.Lookup(tk.Key)
	if !ok || got.ConversationID != 100 || got.ProviderID != 1 {
		t.Errorf("Lookup mismatch: %+v ok=%v", got, ok)
	}

	// Revoke 后 Lookup 失败。
	s.Revoke(tk.Key)
	if _, ok := s.Lookup(tk.Key); ok {
		t.Error("Lookup after Revoke should fail")
	}
}

// TestKeyStoreExpiry 验证过期 key 校验失败。
func TestKeyStoreExpiry(t *testing.T) {
	s := NewKeyStore()
	tk := s.Issue(1, 1, 1, "m", 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	if _, ok := s.Lookup(tk.Key); ok {
		t.Error("expired key should fail Lookup")
	}
}

// TestKeyStoreRevokeByConversation 验证按 conversation 撤销。
func TestKeyStoreRevokeByConversation(t *testing.T) {
	s := NewKeyStore()
	tk1 := s.Issue(5, 1, 1, "m", time.Hour)
	tk2 := s.Issue(5, 2, 1, "m", time.Hour)
	tk3 := s.Issue(6, 3, 1, "m", time.Hour)

	s.RevokeByConversation(5)
	if _, ok := s.Lookup(tk1.Key); ok {
		t.Error("tk1 (conv 5) should be revoked")
	}
	if _, ok := s.Lookup(tk2.Key); ok {
		t.Error("tk2 (conv 5) should be revoked")
	}
	if _, ok := s.Lookup(tk3.Key); !ok {
		t.Error("tk3 (conv 6) should still be valid")
	}
}

// TestKeyStoreLookupInvalid 验证无效 key 返回 false。
func TestKeyStoreLookupInvalid(t *testing.T) {
	s := NewKeyStore()
	if _, ok := s.Lookup("llmk_nonexistent"); ok {
		t.Error("nonexistent key should fail")
	}
	if _, ok := s.Lookup("not-even-a-key"); ok {
		t.Error("malformed key should fail")
	}
}

// 确保导入 llm 包（TempLLMKey 类型用）。
var _ llm.TempLLMKey
