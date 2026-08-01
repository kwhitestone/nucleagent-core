package llmproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
)

// masterKey 从 MASTER_KEY 环境变量读取（32 字节 hex 或 32 字节原始）。
// 用于 Provider.api_key 的 AES-GCM 加解密。
func masterKey() ([]byte, error) {
	s := os.Getenv("MASTER_KEY")
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
