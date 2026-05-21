package main

import (
	"strings"
	"testing"
)

func TestWebhookSecretStoredEncrypted(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("e", 32))
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("InitializeTestDB() error = %v", err)
	}
	if err := db.Where("provider = ?", paperlessWebhookProvider).Delete(&WebhookSecret{}).Error; err != nil {
		t.Fatalf("reset webhook secret: %v", err)
	}
	app := &App{Database: db}

	if err := app.upsertWebhookSecret(t.Context(), paperlessWebhookProvider, "webhook-secret"); err != nil {
		t.Fatalf("upsert webhook secret: %v", err)
	}

	var raw WebhookSecret
	if err := db.Where("provider = ?", paperlessWebhookProvider).First(&raw).Error; err != nil {
		t.Fatalf("load webhook secret: %v", err)
	}
	if raw.Secret == "webhook-secret" || !IsEncryptedSecret(raw.Secret) {
		t.Fatalf("secret was not encrypted at rest: %q", raw.Secret)
	}

	secret, err := app.getWebhookSecret(t.Context(), paperlessWebhookProvider)
	if err != nil {
		t.Fatalf("get webhook secret: %v", err)
	}
	if secret != "webhook-secret" {
		t.Fatalf("secret = %q, want webhook-secret", secret)
	}
}

func TestWebhookSecretLegacyPlaintextUpgrade(t *testing.T) {
	t.Setenv("PAPERLESS_GPT_SECRET_KEY", strings.Repeat("f", 32))
	db, err := InitializeTestDB()
	if err != nil {
		t.Fatalf("InitializeTestDB() error = %v", err)
	}
	if err := db.Where("provider = ?", paperlessWebhookProvider).Delete(&WebhookSecret{}).Error; err != nil {
		t.Fatalf("reset webhook secret: %v", err)
	}
	if err := db.Create(&WebhookSecret{
		Provider: paperlessWebhookProvider,
		Secret:   "legacy-webhook-secret",
		Enabled:  true,
	}).Error; err != nil {
		t.Fatalf("seed legacy webhook secret: %v", err)
	}
	app := &App{Database: db}

	secret, err := app.getWebhookSecret(t.Context(), paperlessWebhookProvider)
	if err != nil {
		t.Fatalf("get webhook secret: %v", err)
	}
	if secret != "legacy-webhook-secret" {
		t.Fatalf("secret = %q, want legacy-webhook-secret", secret)
	}

	var raw WebhookSecret
	if err := db.Where("provider = ?", paperlessWebhookProvider).First(&raw).Error; err != nil {
		t.Fatalf("load upgraded webhook secret: %v", err)
	}
	if !IsEncryptedSecret(raw.Secret) {
		t.Fatalf("legacy secret was not upgraded")
	}
}
