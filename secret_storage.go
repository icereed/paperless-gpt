package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const encryptedSecretPrefix = "enc:v1:"

var secretKeyMaterialOverride func() ([]byte, error)

func secretKeyMaterial() ([]byte, error) {
	if secretKeyMaterialOverride != nil {
		return secretKeyMaterialOverride()
	}
	if raw := strings.TrimSpace(os.Getenv("PAPERLESS_GPT_SECRET_KEY")); raw != "" {
		if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) >= 32 {
			return decoded[:32], nil
		}
		if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) >= 32 {
			return decoded[:32], nil
		}
		sum := sha256.Sum256([]byte(raw))
		return sum[:], nil
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(configDir, "secret.key")
	if data, err := os.ReadFile(keyPath); err == nil {
		decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if decodeErr != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("invalid local secret key at %s", keyPath)
		}
		return decoded, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func EncryptSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := secretKeyMaterial()
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
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return encryptedSecretPrefix + base64.StdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func DecryptSecret(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !IsEncryptedSecret(ciphertext) {
		return "", errors.New("secret is not encrypted")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, encryptedSecretPrefix))
	if err != nil {
		return "", err
	}
	key, err := secretKeyMaterial()
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
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("encrypted secret payload is too short")
	}
	nonce, encrypted := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func IsEncryptedSecret(value string) bool {
	return strings.HasPrefix(value, encryptedSecretPrefix)
}

func EncryptSecretForStorage(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if IsEncryptedSecret(value) {
		return value, nil
	}
	return EncryptSecret(value)
}

func DecryptSecretFromStorage(value string) (plaintext string, legacyPlaintext bool, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, nil
	}
	if !IsEncryptedSecret(value) {
		return value, true, nil
	}
	plaintext, err = DecryptSecret(value)
	return plaintext, false, err
}
