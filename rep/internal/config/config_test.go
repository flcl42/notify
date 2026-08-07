package config

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestLoadSaveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rep.yaml")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if cfg.DefaultPairPort != 8788 {
		t.Fatalf("default port mismatch: %d", cfg.DefaultPairPort)
	}
	if len(cfg.Subscriptions) != 0 {
		t.Fatalf("expected no subscriptions")
	}

	cfg.FcmServiceAccount = "/secure/service-account.json"
	cfg.Subscriptions = []Subscription{
		{
			ID:           uuid.Must(uuid.NewRandom()).String(),
			Title:        "Build Alerts",
			Name:         "Build Alerts",
			DefaultTitle: "Build Alerts",
			Key:          "test-key",
			Delivery:     "push",
			PushTokens:   []PushToken{},
			CreatedAt:    "2024-01-01T00:00:00Z",
		},
	}
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.FcmServiceAccount != cfg.FcmServiceAccount {
		t.Fatalf("credential path mismatch")
	}
	if len(loaded.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(loaded.Subscriptions))
	}
	if loaded.Subscriptions[0].Title != "Build Alerts" {
		t.Fatalf("title mismatch")
	}
}

func TestUpsertAndFind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rep.yaml")

	sub := Subscription{
		ID:           uuid.Must(uuid.NewRandom()).String(),
		Title:        "Build Alerts",
		Name:         "Build Alerts",
		DefaultTitle: "Build Alerts",
		Key:          "key1",
		CreatedAt:    "2024-01-01T00:00:00Z",
	}
	if _, err := UpsertSubscription(path, sub, false); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	_, err := UpsertSubscription(path, sub, false)
	if err == nil {
		t.Fatalf("expected duplicate title error")
	}

	found := FindSubscriptionByTitle(MustLoad(path), "build alerts")
	if found == nil || found.Key != "key1" {
		t.Fatalf("find subscription failed")
	}

	sub.Key = "key2"
	if _, err := UpsertSubscription(path, sub, true); err != nil {
		t.Fatalf("upsert replace: %v", err)
	}
	found = FindSubscriptionByTitle(MustLoad(path), "Build Alerts")
	if found == nil || found.Key != "key2" {
		t.Fatalf("replace failed")
	}
}

func TestAddPushRegistration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rep.yaml")

	sub := Subscription{
		ID:           uuid.Must(uuid.NewRandom()).String(),
		Title:        "Build Alerts",
		Name:         "Build Alerts",
		DefaultTitle: "Build Alerts",
		Key:          "key1",
		CreatedAt:    "2024-01-01T00:00:00Z",
	}
	if _, err := UpsertSubscription(path, sub, false); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	_, updated, err := AddPushRegistration(path, sub.ID, PushToken{Provider: "fcm", Token: "token1", Platform: "android"})
	if err != nil {
		t.Fatalf("add token: %v", err)
	}
	if len(updated.PushTokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(updated.PushTokens))
	}

	_, updated, err = AddPushRegistration(path, sub.ID, PushToken{Provider: "fcm", Token: "token1", Platform: "android"})
	if err != nil {
		t.Fatalf("add same token: %v", err)
	}
	if len(updated.PushTokens) != 1 {
		t.Fatalf("expected 1 token after update, got %d", len(updated.PushTokens))
	}

	_, updated, err = AddPushRegistration(path, sub.ID, PushToken{Provider: "fcm", Token: "token2", Platform: "android"})
	if err != nil {
		t.Fatalf("add second token: %v", err)
	}
	if len(updated.PushTokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(updated.PushTokens))
	}
}

func MustLoad(path string) Config {
	cfg, err := LoadConfig(path)
	if err != nil {
		panic(err)
	}
	return cfg
}
