package main

import (
	"os"
	"strings"
	"testing"
)

func TestEncryptSecretRoundTrip(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("a", 32))

	encrypted, err := EncryptSecret("super-secret")
	if err != nil {
		t.Fatalf("EncryptSecret() error = %v", err)
	}
	if encrypted == "super-secret" || !IsEncryptedSecret(encrypted) {
		t.Fatalf("expected encrypted value with prefix, got %q", encrypted)
	}

	decrypted, err := DecryptSecret(encrypted)
	if err != nil {
		t.Fatalf("DecryptSecret() error = %v", err)
	}
	if decrypted != "super-secret" {
		t.Fatalf("decrypted = %q, want super-secret", decrypted)
	}
}

func TestDecryptSecretWrongKeyFails(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("a", 32))
	encrypted, err := EncryptSecret("super-secret")
	if err != nil {
		t.Fatalf("EncryptSecret() error = %v", err)
	}

	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("b", 32))
	if _, err := DecryptSecret(encrypted); err == nil {
		t.Fatal("expected wrong key to fail")
	}
}

func TestDecryptSecretMalformedFails(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("a", 32))
	if _, err := DecryptSecret("enc:v1:not-base64"); err == nil {
		t.Fatal("expected malformed encrypted secret to fail")
	}
	if _, err := DecryptSecret("plain"); err == nil {
		t.Fatal("expected plaintext secret to fail")
	}
}

func TestEmptySecret(t *testing.T) {
	encrypted, err := EncryptSecret("")
	if err != nil {
		t.Fatalf("EncryptSecret(empty) error = %v", err)
	}
	if encrypted != "" {
		t.Fatalf("EncryptSecret(empty) = %q, want empty", encrypted)
	}
	decrypted, err := DecryptSecret("")
	if err != nil {
		t.Fatalf("DecryptSecret(empty) error = %v", err)
	}
	if decrypted != "" {
		t.Fatalf("DecryptSecret(empty) = %q, want empty", decrypted)
	}
}

func TestSecretStorageLegacyPlaintextFallback(t *testing.T) {
	plaintext, legacy, err := DecryptSecretFromStorage(" legacy-secret ")
	if err != nil {
		t.Fatalf("DecryptSecretFromStorage() error = %v", err)
	}
	if plaintext != "legacy-secret" || !legacy {
		t.Fatalf("got plaintext=%q legacy=%v, want legacy-secret true", plaintext, legacy)
	}
}

func TestEncryptSecretForStoragePreservesEncryptedValue(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("a", 32))
	encrypted, err := EncryptSecret("super-secret")
	if err != nil {
		t.Fatalf("EncryptSecret() error = %v", err)
	}
	stored, err := EncryptSecretForStorage(encrypted)
	if err != nil {
		t.Fatalf("EncryptSecretForStorage() error = %v", err)
	}
	if stored != encrypted {
		t.Fatalf("encrypted value was changed")
	}
}

func TestLocalSecretKeyIsPersistedOwnerOnly(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", "")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	first, err := secretKeyMaterial()
	if err != nil {
		t.Fatalf("secretKeyMaterial() error = %v", err)
	}
	second, err := secretKeyMaterial()
	if err != nil {
		t.Fatalf("secretKeyMaterial() second error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("expected persisted local key to be reused")
	}
	info, err := os.Stat("config/secret.key")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("secret key mode = %v, want 0600", info.Mode().Perm())
	}
}
