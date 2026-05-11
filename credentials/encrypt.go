package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

const gcmNonceSize = 12

// EncryptPayload returns nonce and ciphertext (includes GCM tag) using AES-256-GCM.
func EncryptPayload(dek, plaintext []byte) (nonceB64, payloadB64 string, err error) {
	if len(dek) != 32 {
		return "", "", fmt.Errorf("credentials: DEK must be 32 bytes, got %d", len(dek))
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptPayload reverses EncryptPayload.
func DecryptPayload(dek []byte, nonceB64, payloadB64 string) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("credentials: DEK must be 32 bytes, got %d", len(dek))
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, fmt.Errorf("credentials: decode nonce: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("credentials: decode payload: %w", err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("credentials: decrypt failed: %w", err)
	}
	return plain, nil
}
