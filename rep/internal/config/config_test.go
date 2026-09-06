package config

import (
	"os"
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
	if cfg.Mode != ModeServer || cfg.ServerURL != DefaultServerURL {
		t.Fatalf("unexpected defaults: mode=%q server=%q", cfg.Mode, cfg.ServerURL)
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

func TestResolvePairingBaseURL(t *testing.T) {
	if actual := ResolvePairingBaseURL(DefaultServerURL); actual != DefaultPairingURL {
		t.Fatalf("default pairing URL = %q, want %q", actual, DefaultPairingURL)
	}
	if actual := ResolvePairingBaseURL("https://relay.example/"); actual != "https://relay.example" {
		t.Fatalf("custom pairing URL = %q", actual)
	}
}

func TestLoadMigratesLegacyHostedRelayURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rep.yaml")
	if err := os.WriteFile(path, []byte("v: 1\nmode: server\nserverUrl: http://62.171.163.96:17891\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != DefaultServerURL {
		t.Fatalf("legacy relay URL was not migrated: %q", cfg.ServerURL)
	}
	if !IsHostedRelayURL("http://62.171.163.96:17891/") || !IsHostedRelayURL(DefaultServerURL) {
		t.Fatal("hosted relay URL detection failed")
	}
}

func TestMigrateLegacyConfigFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyConfigName)
	targetPath := filepath.Join(dir, defaultConfigName)
	legacy := []byte("v: 1\nmode: server\nserverUrl: https://notify.apps.flcl.me\n")
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	actualPath, err := migrateLegacyConfig(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if actualPath != targetPath {
		t.Fatalf("migrated path = %q, want %q", actualPath, targetPath)
	}
	migrated, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(migrated) != string(legacy) {
		t.Fatalf("migrated config changed contents: %q", migrated)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy config should remain available: %v", err)
	}

	if err := os.WriteFile(targetPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrateLegacyConfig(targetPath); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(targetPath)
	if string(unchanged) != "existing" {
		t.Fatalf("existing nfy config was overwritten: %q", unchanged)
	}
}

func TestNfyEnvironmentVariablesTakePrecedence(t *testing.T) {
	t.Setenv("REP_MODE", ModeDirect)
	t.Setenv("NFY_MODE", ModeServer)
	t.Setenv("REP_SERVER_URL", "https://legacy.example")
	t.Setenv("NFY_SERVER_URL", "https://nfy.example")
	t.Setenv("REP_FCM_SERVICE_ACCOUNT", "/legacy.json")
	t.Setenv("NFY_FCM_SERVICE_ACCOUNT", "/nfy.json")

	mode, err := ResolveMode(DefaultConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != ModeServer {
		t.Fatalf("mode = %q, want %q", mode, ModeServer)
	}
	if serverURL := ResolveServerURL(DefaultConfig(), ""); serverURL != "https://nfy.example" {
		t.Fatalf("server URL = %q", serverURL)
	}
	if credential := ResolveFcmServiceAccount(DefaultConfig(), ""); credential != "/nfy.json" {
		t.Fatalf("credential = %q", credential)
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
