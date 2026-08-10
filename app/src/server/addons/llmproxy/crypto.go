package llmproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"whitestone.top/prism-fusion/global"
)

// masterKeyRaw 取主密钥原始字符串。
//
// 优先走 viper（config.yaml 的 nucleagent.master-key，由 main.go 的
// expandNucleagentEnv 展开 ${MASTER_KEY}），与 JWT_SECRET / EXECUTOR_TOKEN
// 走同一条配置路径——历史上本值绕过配置体系直接读 env，导致它从未被写进
// .env，只靠某个终端的手动 export 存活，重启即静默丢失。
//
// 回退到 os.Getenv 有两个用途：(1) 单元测试直接 os.Setenv，不初始化 viper；
// (2) 容器/CI 里只注入环境变量、不挂 config.yaml 的场景。
func masterKeyRaw() string {
	if global.PRISM_VP != nil {
		s := strings.TrimSpace(global.PRISM_VP.GetString("nucleagent.master-key"))
		// 未被展开的字面量（如 "${MASTER_KEY}"）视为未配置，落到 env 回退。
		if s != "" && !strings.Contains(s, "${") {
			return s
		}
	}
	return strings.TrimSpace(os.Getenv("MASTER_KEY"))
}

// ValidateMasterKey 供启动期自检：主密钥缺失/格式错误时返回 error。
//
// core 必须在启动时 fail fast——这个值不可能在运行时被补上，带着它跑起来
// 只会把失败推迟到用户发消息的那一刻，且表现为难以定位的 LLM 调用失败。
func ValidateMasterKey() error {
	_, err := masterKey()
	return err
}

// masterKey 解析主密钥为 32 字节（64 位 hex 或 32 字节原始）。
// 用于 Provider.api_key 的 AES-GCM 加解密。
func masterKey() ([]byte, error) {
	s := masterKeyRaw()
	if s == "" {
		return nil, errors.New("llmproxy: MASTER_KEY not set")
	}
	// 优先按 hex 解析。
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(s) == 32 {
		return []byte(s), nil
	}
	return nil, errors.New("llmproxy: MASTER_KEY must be 32 bytes (hex or raw)")
}

// EncryptAPIKey 用 MASTER_KEY 加密 API key（AES-GCM），返回 hex 编码的密文。
func EncryptAPIKey(plain string) (string, error) {
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return hex.EncodeToString(sealed), nil
}

// DecryptAPIKey 解密 EncryptAPIKey 产出的密文。
func DecryptAPIKey(cipherHex string) (string, error) {
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	data, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("llmproxy: ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
