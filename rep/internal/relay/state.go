package relay

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flcl42/notify/rep/internal/fcm"
)

var (
	ErrUnknownSubscription = errors.New("unknown subscription")
	ErrPublicKeyConflict   = errors.New("subscription is already bound to another key")
	ErrRegistrationDenied  = errors.New("invalid registration token")
	ErrReplay              = errors.New("request nonce was already used")
)

type RateLimitError struct {
	Scope string
}

func (e *RateLimitError) Error() string {
	if e.Scope == "source IP notification keys" {
		return "source IP daily notification-key limit exceeded"
	}
	return e.Scope + " daily notification limit exceeded"
}

type storedPushToken struct {
	Provider     string `json:"provider"`
	Token        string `json:"token"`
	Platform     string `json:"platform,omitempty"`
	RegisteredAt string `json:"registeredAt,omitempty"`
}

type storedSubscription struct {
	PublicKey       string            `json:"publicKey"`
	RegistrationKey string            `json:"registrationKey"`
	PushTokens      []storedPushToken `json:"pushTokens"`
	CreatedAt       string            `json:"createdAt"`
}

type dailyCounters struct {
	Day                string                     `json:"day"`
	Total              int                        `json:"total"`
	PerSubscription    map[string]int             `json:"perSubscription"`
	PerIPSubscriptions map[string]map[string]bool `json:"perIpSubscriptions"`
}

type persistentState struct {
	V             int                            `json:"v"`
	Subscriptions map[string]*storedSubscription `json:"subscriptions"`
	Counters      dailyCounters                  `json:"counters"`
	UsedNonces    map[string]int64               `json:"usedNonces"`
}

type Store struct {
	mu                       sync.Mutex
	path                     string
	dailyLimit               int
	subscriptionDailyLimit   int
	ipSubscriptionDailyLimit int
	state                    persistentState
}

type LimitResult struct {
	DailyRemaining             int
	SubscriptionDailyRemaining int
}

func OpenStore(path string, dailyLimit, subscriptionDailyLimit, ipSubscriptionDailyLimit int) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("relay state path is required")
	}
	if dailyLimit <= 0 || subscriptionDailyLimit <= 0 || ipSubscriptionDailyLimit <= 0 {
		return nil, fmt.Errorf("daily limits must be positive")
	}
	store := &Store{
		path:                     path,
		dailyLimit:               dailyLimit,
		subscriptionDailyLimit:   subscriptionDailyLimit,
		ipSubscriptionDailyLimit: ipSubscriptionDailyLimit,
		state: persistentState{
			V:             1,
			Subscriptions: map[string]*storedSubscription{},
			Counters: dailyCounters{
				PerSubscription:    map[string]int{},
				PerIPSubscriptions: map[string]map[string]bool{},
			},
			UsedNonces: map[string]int64{},
		},
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &store.state); err != nil {
			return nil, fmt.Errorf("parse relay state %s: %w", path, err)
		}
		store.normalizeLocked()
		return store, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := store.saveLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) normalizeLocked() {
	if s.state.V == 0 {
		s.state.V = 1
	}
	if s.state.Subscriptions == nil {
		s.state.Subscriptions = map[string]*storedSubscription{}
	}
	if s.state.Counters.PerSubscription == nil {
		s.state.Counters.PerSubscription = map[string]int{}
	}
	if s.state.Counters.PerIPSubscriptions == nil {
		s.state.Counters.PerIPSubscriptions = map[string]map[string]bool{}
	}
	if s.state.UsedNonces == nil {
		s.state.UsedNonces = map[string]int64{}
	}
	for _, subscription := range s.state.Subscriptions {
		if subscription.PushTokens == nil {
			subscription.PushTokens = []storedPushToken{}
		}
	}
}

func (s *Store) Provision(subscriptionID, publicKey string, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing := s.state.Subscriptions[subscriptionID]; existing != nil {
		if subtle.ConstantTimeCompare([]byte(existing.PublicKey), []byte(publicKey)) != 1 {
			return "", ErrPublicKeyConflict
		}
		return existing.RegistrationKey, nil
	}

	registrationBytes := make([]byte, 32)
	if _, err := rand.Read(registrationBytes); err != nil {
		return "", err
	}
	registrationKey := base64.RawURLEncoding.EncodeToString(registrationBytes)
	s.state.Subscriptions[subscriptionID] = &storedSubscription{
		PublicKey:       publicKey,
		RegistrationKey: registrationKey,
		PushTokens:      []storedPushToken{},
		CreatedAt:       now.UTC().Format(time.RFC3339Nano),
	}
	if err := s.saveLocked(); err != nil {
		delete(s.state.Subscriptions, subscriptionID)
		return "", err
	}
	return registrationKey, nil
}

func (s *Store) PublicKey(subscriptionID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscription := s.state.Subscriptions[subscriptionID]
	if subscription == nil {
		return "", ErrUnknownSubscription
	}
	return subscription.PublicKey, nil
}

func (s *Store) Register(subscriptionID, registrationKey string, registration RegistrationRequest, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscription := s.state.Subscriptions[subscriptionID]
	if subscription == nil {
		return ErrUnknownSubscription
	}
	if subtle.ConstantTimeCompare([]byte(subscription.RegistrationKey), []byte(registrationKey)) != 1 {
		return ErrRegistrationDenied
	}

	previous := append([]storedPushToken(nil), subscription.PushTokens...)
	registered := storedPushToken{
		Provider:     registration.Provider,
		Token:        registration.PushToken,
		Platform:     registration.Platform,
		RegisteredAt: now.UTC().Format(time.RFC3339Nano),
	}
	updated := false
	for i := range subscription.PushTokens {
		if subscription.PushTokens[i].Token == registered.Token {
			subscription.PushTokens[i] = registered
			updated = true
			break
		}
	}
	if !updated {
		subscription.PushTokens = append([]storedPushToken{registered}, subscription.PushTokens...)
	}
	if err := s.saveLocked(); err != nil {
		subscription.PushTokens = previous
		return err
	}
	return nil
}

func (s *Store) RegisteredTokenCount(subscriptionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscription := s.state.Subscriptions[subscriptionID]
	if subscription == nil {
		return 0, ErrUnknownSubscription
	}
	count := 0
	for _, token := range subscription.PushTokens {
		if token.Provider == "fcm" && token.Token != "" {
			count++
		}
	}
	return count, nil
}

func (s *Store) MergePushTokens(subscriptionID string, supplied []fcm.PushToken) ([]fcm.PushToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscription := s.state.Subscriptions[subscriptionID]
	if subscription == nil {
		return nil, ErrUnknownSubscription
	}

	byToken := map[string]fcm.PushToken{}
	for _, token := range subscription.PushTokens {
		if token.Provider == "fcm" && token.Token != "" {
			byToken[token.Token] = fcm.PushToken{Provider: token.Provider, Token: token.Token}
		}
	}
	for _, token := range supplied {
		if token.Provider == "fcm" && token.Token != "" {
			byToken[token.Token] = token
		}
	}

	keys := make([]string, 0, len(byToken))
	for token := range byToken {
		keys = append(keys, token)
	}
	sort.Strings(keys)
	result := make([]fcm.PushToken, 0, len(keys))
	for _, token := range keys {
		result = append(result, byToken[token])
	}
	return result, nil
}

func (s *Store) Reserve(subscriptionID, nonce, sourceID string, deliveries int, now time.Time) (LimitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Subscriptions[subscriptionID] == nil {
		return LimitResult{}, ErrUnknownSubscription
	}
	if strings.TrimSpace(sourceID) == "" {
		return LimitResult{}, fmt.Errorf("source id is required")
	}
	if deliveries <= 0 {
		return LimitResult{}, fmt.Errorf("delivery count must be positive")
	}

	previousCounters := cloneCounters(s.state.Counters)
	previousNonces := cloneNonces(s.state.UsedNonces)
	day := now.UTC().Format("2006-01-02")
	if s.state.Counters.Day != day {
		s.state.Counters = dailyCounters{
			Day:                day,
			PerSubscription:    map[string]int{},
			PerIPSubscriptions: map[string]map[string]bool{},
		}
	}
	cutoff := now.UTC().Add(-10 * time.Minute).Unix()
	for key, usedAt := range s.state.UsedNonces {
		if usedAt < cutoff {
			delete(s.state.UsedNonces, key)
		}
	}
	replayKey := subscriptionID + ":" + nonce
	if _, exists := s.state.UsedNonces[replayKey]; exists {
		return LimitResult{}, ErrReplay
	}
	if s.state.Counters.Total+deliveries > s.dailyLimit {
		return LimitResult{}, &RateLimitError{Scope: "global"}
	}
	if s.state.Counters.PerSubscription[subscriptionID]+deliveries > s.subscriptionDailyLimit {
		return LimitResult{}, &RateLimitError{Scope: "subscription"}
	}
	sourceSubscriptions := s.state.Counters.PerIPSubscriptions[sourceID]
	if sourceSubscriptions == nil {
		sourceSubscriptions = map[string]bool{}
	}
	if !sourceSubscriptions[subscriptionID] && len(sourceSubscriptions) >= s.ipSubscriptionDailyLimit {
		return LimitResult{}, &RateLimitError{Scope: "source IP notification keys"}
	}

	s.state.UsedNonces[replayKey] = now.UTC().Unix()
	s.state.Counters.Total += deliveries
	s.state.Counters.PerSubscription[subscriptionID] += deliveries
	sourceSubscriptions[subscriptionID] = true
	s.state.Counters.PerIPSubscriptions[sourceID] = sourceSubscriptions
	result := LimitResult{
		DailyRemaining:             s.dailyLimit - s.state.Counters.Total,
		SubscriptionDailyRemaining: s.subscriptionDailyLimit - s.state.Counters.PerSubscription[subscriptionID],
	}
	if err := s.saveLocked(); err != nil {
		s.state.Counters = previousCounters
		s.state.UsedNonces = previousNonces
		return LimitResult{}, err
	}
	return result, nil
}

func cloneCounters(value dailyCounters) dailyCounters {
	copyValue := dailyCounters{
		Day:                value.Day,
		Total:              value.Total,
		PerSubscription:    map[string]int{},
		PerIPSubscriptions: map[string]map[string]bool{},
	}
	for key, count := range value.PerSubscription {
		copyValue.PerSubscription[key] = count
	}
	for sourceID, subscriptions := range value.PerIPSubscriptions {
		copyValue.PerIPSubscriptions[sourceID] = map[string]bool{}
		for subscriptionID, used := range subscriptions {
			copyValue.PerIPSubscriptions[sourceID][subscriptionID] = used
		}
	}
	return copyValue
}

func cloneNonces(value map[string]int64) map[string]int64 {
	copyValue := make(map[string]int64, len(value))
	for key, timestamp := range value {
		copyValue[key] = timestamp
	}
	return copyValue
}

func (s *Store) saveLocked() error {
	s.normalizeLocked()
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".notify-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(s.path)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		cleanup()
		return err
	}
	return os.Chmod(s.path, 0o600)
}
