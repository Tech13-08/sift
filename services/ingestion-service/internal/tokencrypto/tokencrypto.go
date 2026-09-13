package tokencrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const Prefix = "enc:v1:"

const nonceSize = 12
const tagSize = 16

func ParseKey(raw string) ([]byte, error) {
	hexKey := strings.TrimSpace(raw)
	if hexKey == "" {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY is required")
	}
	if len(hexKey) != 64 {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY must be 64 hex characters")
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY must be hex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY must decode to 32 bytes")
	}
	return key, nil
}

func IsCiphertext(stored string) bool {
	return strings.HasPrefix(stored, Prefix)
}

func Encrypt(key []byte, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if IsCiphertext(plaintext) {
		return plaintext, nil
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return encryptWithNonce(key, nonce, plaintext)
}

func Decrypt(key []byte, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !IsCiphertext(stored) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, Prefix))
	if err != nil {
		return "", fmt.Errorf("token ciphertext: %w", err)
	}
	if len(raw) < nonceSize+tagSize {
		return "", fmt.Errorf("token ciphertext too short")
	}
	nonce := raw[:nonceSize]
	payload := raw[nonceSize:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("token decrypt failed (wrong TOKEN_ENCRYPTION_KEY?): %w", err)
	}
	return string(plain), nil
}

func encryptWithNonce(key, nonce []byte, plaintext string) (string, error) {
	if len(nonce) != nonceSize {
		return "", fmt.Errorf("nonce must be %d bytes", nonceSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, nonceSize+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return Prefix + base64.StdEncoding.EncodeToString(out), nil
}
