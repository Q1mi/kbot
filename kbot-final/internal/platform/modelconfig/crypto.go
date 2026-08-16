package modelconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// Cipher 负责 API Key 的应用层加密。数据库与 API 都不接触明文回显。
type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(secret []byte) (*Cipher, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("empty credential encryption key")
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (c *Cipher) Decrypt(ciphertext []byte) (string, error) {
	n := c.aead.NonceSize()
	if len(ciphertext) < n {
		return "", fmt.Errorf("invalid credential ciphertext")
	}
	plain, err := c.aead.Open(nil, ciphertext[:n], ciphertext[n:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt api key: %w", err)
	}
	return string(plain), nil
}
