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

const (
	defaultConfigName = "nfy.yaml"
	legacyConfigName  = "rep.yaml"
)

const (
	ModeServer        = "server"
	ModeDirect        = "direct"
	DefaultServerURL  = "https://notify.apps.flcl.me"
	DefaultPairingURL = "https://notify.apps.flcl.me"
	legacyServerURL   = "http://62.171.163.96:17891"
)

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
	Mode              string         `yaml:"mode"`
	ServerURL         string         `yaml:"serverUrl"`
	FcmServiceAccount string         `yaml:"fcmServiceAccount,omitempty"`
	Subscriptions     []Subscription `yaml:"subscriptions"`
}

func DefaultConfig() Config {
	return Config{
		V:               1,
		DefaultPairPort: 8788,
		Mode:            ModeServer,
		ServerURL:       DefaultServerURL,
		Subscriptions:   []Subscription{},
	}
}

func GetConfigPath() (string, error) {
	if env := os.Getenv("NFY_CONFIG"); env != "" {
		return env, nil
	}
	if env := os.Getenv("REP_CONFIG"); env != "" {
		return env, nil
	}

	exe, err := os.Executable()
	if err == nil {
		base := strings.ToLower(filepath.Base(exe))
		ext := filepath.Ext(base)
		name := strings.TrimSuffix(base, ext)
		// Keep renamed nfy releases and the legacy rep compatibility binary on one config.
		if name == "nfy" || strings.HasPrefix(name, "nfy-") || name == "rep" || strings.HasPrefix(name, "rep-") {
			return migrateLegacyConfig(filepath.Join(filepath.Dir(exe), defaultConfigName))
		}
	}

	if runtime.GOOS == "windows" {
		return migrateLegacyConfig(`C:\Programs\` + defaultConfigName)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config path: %w", err)
	}
	return migrateLegacyConfig(filepath.Join(home, ".config", "nfy", defaultConfigName))
}

func migrateLegacyConfig(targetPath string) (string, error) {
	if _, err := os.Stat(targetPath); err == nil {
		return targetPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	candidates := []string{filepath.Join(filepath.Dir(targetPath), legacyConfigName)}
	if home, err := os.UserHomeDir(); err == nil {
		defaultUserPath := filepath.Join(home, ".config", "nfy", defaultConfigName)
		if filepath.Clean(targetPath) == filepath.Clean(defaultUserPath) {
			candidates = append(candidates, filepath.Join(home, ".config", "private-notify", legacyConfigName))
		}
	}
	for _, legacyPath := range candidates {
		if filepath.Clean(legacyPath) == filepath.Clean(targetPath) {
			continue
		}
		data, err := os.ReadFile(legacyPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read legacy config %s: %w", legacyPath, err)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", err
		}
		file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return targetPath, nil
		}
		if err != nil {
			return "", fmt.Errorf("create migrated config %s: %w", targetPath, err)
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(targetPath)
			return "", fmt.Errorf("migrate config to %s: %w", targetPath, err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(targetPath)
			return "", fmt.Errorf("close migrated config %s: %w", targetPath, err)
		}
		return targetPath, nil
	}
	return targetPath, nil
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

	cfg = Normalize(cfg)
	return cfg, nil
}

func SaveConfig(path string, cfg Config) error {
	cfg = Normalize(cfg)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Normalize(cfg Config) Config {
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = ModeServer
	}
	if strings.TrimSpace(cfg.ServerURL) == "" || IsHostedRelayURL(cfg.ServerURL) {
		cfg.ServerURL = DefaultServerURL
	} else {
		cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	}
	cfg.Subscriptions = normalizeSubscriptions(cfg.Subscriptions)
	return cfg
}

func ResolveMode(cfg Config, explicit string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(explicit))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(firstEnvironmentValue("NFY_MODE", "REP_MODE")))
	}
	if mode == "" {
		mode = cfg.Mode
	}
	if mode == "" {
		mode = ModeServer
	}
	if mode != ModeServer && mode != ModeDirect {
		return "", fmt.Errorf("unsupported delivery mode %q; use server or direct", mode)
	}
	return mode, nil
}

func ResolveServerURL(cfg Config, explicit string) string {
	serverURL := strings.TrimSpace(explicit)
	if serverURL == "" {
		serverURL = strings.TrimSpace(firstEnvironmentValue("NFY_SERVER_URL", "REP_SERVER_URL"))
	}
	if serverURL == "" {
		serverURL = cfg.ServerURL
	}
	if serverURL == "" || IsHostedRelayURL(serverURL) {
		serverURL = DefaultServerURL
	}
	return strings.TrimRight(serverURL, "/")
}

func ResolvePairingBaseURL(serverURL string) string {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if IsHostedRelayURL(serverURL) {
		return DefaultPairingURL
	}
	return serverURL
}

func IsHostedRelayURL(serverURL string) bool {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	return strings.EqualFold(serverURL, DefaultServerURL) || strings.EqualFold(serverURL, legacyServerURL)
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
	if env := firstEnvironmentValue("NFY_FCM_SERVICE_ACCOUNT", "REP_FCM_SERVICE_ACCOUNT"); env != "" {
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

func firstEnvironmentValue(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
