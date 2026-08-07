package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultConfigName = "rep.yaml"

type PushToken struct {
	Provider     string `yaml:"provider"`
	Token        string `yaml:"token"`
	Platform     string `yaml:"platform"`
	RegisteredAt string `yaml:"registeredAt"`
}

type Subscription struct {
	ID           string      `yaml:"id"`
	Title        string      `yaml:"title"`
	Name         string      `yaml:"name"`
	DefaultTitle string      `yaml:"defaultTitle"`
	Key          string      `yaml:"key"`
	Delivery     string      `yaml:"delivery"`
	PushTokens   []PushToken `yaml:"pushTokens"`
	CreatedAt    string      `yaml:"createdAt"`
}

type Config struct {
	V                 int            `yaml:"v"`
	DefaultPairPort   int            `yaml:"defaultPairPort"`
	FcmServiceAccount string         `yaml:"fcmServiceAccount,omitempty"`
	Subscriptions     []Subscription `yaml:"subscriptions"`
}

func DefaultConfig() Config {
	return Config{
		V:               1,
		DefaultPairPort: 8788,
		Subscriptions:   []Subscription{},
	}
}

func GetConfigPath() (string, error) {
	if env := os.Getenv("REP_CONFIG"); env != "" {
		return env, nil
	}

	exe, err := os.Executable()
	if err == nil {
		base := strings.ToLower(filepath.Base(exe))
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		// Release assets are named rep-<os>-<arch>; installed binaries are rep or rep.exe.
		if name == "rep" || strings.HasPrefix(name, "rep-") {
			return filepath.Join(filepath.Dir(exe), defaultConfigName), nil
		}
	}

	if runtime.GOOS == "windows" {
		return `C:\Programs\` + defaultConfigName, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config path: %w", err)
	}
	return filepath.Join(home, ".config", "private-notify", defaultConfigName), nil
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}

	cfg.Subscriptions = normalizeSubscriptions(cfg.Subscriptions)
	return cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	cfg.Subscriptions = normalizeSubscriptions(cfg.Subscriptions)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func normalizeSubscriptions(subs []Subscription) []Subscription {
	if subs == nil {
		return []Subscription{}
	}
	out := make([]Subscription, 0, len(subs))
	for _, sub := range subs {
		title := strings.TrimSpace(sub.Title)
		if title == "" {
			title = strings.TrimSpace(sub.DefaultTitle)
		}
		if title == "" {
			title = strings.TrimSpace(sub.Name)
		}
		if title == "" {
			continue
		}
		if sub.PushTokens == nil {
			sub.PushTokens = []PushToken{}
		}
		sub.Title = title
		if sub.Name == "" {
			sub.Name = title
		}
		if sub.DefaultTitle == "" {
			sub.DefaultTitle = title
		}
		out = append(out, sub)
	}
	return out
}

func FindSubscriptionByTitle(cfg Config, title string) *Subscription {
	wanted := strings.ToLower(strings.TrimSpace(title))
	if wanted == "" {
		return nil
	}
	for i := range cfg.Subscriptions {
		if strings.ToLower(cfg.Subscriptions[i].Title) == wanted {
			return &cfg.Subscriptions[i]
		}
	}
	return nil
}

func UpsertSubscription(path string, sub Subscription, replace bool) (Config, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return cfg, err
	}

	normalized := normalizeSubscriptions([]Subscription{sub})[0]
	existingIdx := -1
	for i := range cfg.Subscriptions {
		if strings.EqualFold(cfg.Subscriptions[i].Title, normalized.Title) {
			existingIdx = i
			break
		}
	}

	if existingIdx >= 0 && !replace {
		return cfg, fmt.Errorf("title already exists: %s. Use --replace to rotate and re-pair it", normalized.Title)
	}

	if existingIdx >= 0 {
		cfg.Subscriptions[existingIdx] = normalized
	} else {
		cfg.Subscriptions = append(cfg.Subscriptions, normalized)
	}

	if err := SaveConfig(path, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func AddPushRegistration(path, subscriptionID string, token PushToken) (Config, *Subscription, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return cfg, nil, err
	}

	idx := -1
	for i := range cfg.Subscriptions {
		if cfg.Subscriptions[i].ID == subscriptionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return cfg, nil, fmt.Errorf("unknown subscription")
	}

	sub := &cfg.Subscriptions[idx]
	existingIdx := -1
	for i := range sub.PushTokens {
		if sub.PushTokens[i].Token == token.Token {
			existingIdx = i
			break
		}
	}

	if existingIdx >= 0 {
		sub.PushTokens[existingIdx] = token
	} else {
		sub.PushTokens = append([]PushToken{token}, sub.PushTokens...)
	}
	sub.Delivery = "push"

	if err := SaveConfig(path, cfg); err != nil {
		return cfg, nil, err
	}
	return cfg, sub, nil
}

func ResolveFcmServiceAccount(cfg Config, explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}
	if env := os.Getenv("REP_FCM_SERVICE_ACCOUNT"); env != "" {
		return env
	}
	if cfg.FcmServiceAccount != "" {
		return cfg.FcmServiceAccount
	}
	if env := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); env != "" {
		return env
	}
	return os.Getenv("FCM_SERVICE_ACCOUNT")
}
