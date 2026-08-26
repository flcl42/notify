package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flcl42/notify/rep/internal/fcm"
	"github.com/flcl42/notify/rep/internal/protocol"
	"github.com/google/uuid"
)

type recordingSender struct {
	mu        sync.Mutex
	requests  int
	envelopes []fcm.Envelope
}

func (s *recordingSender) SendPushNotifications(tokens []fcm.PushToken, envelope fcm.Envelope, service string) (fcm.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.envelopes = append(s.envelopes, envelope)
	return fcm.SendResult{Sent: len(tokens)}, nil
}

func testKey(fill byte) string {
	return protocol.BytesToBase64URL(bytes.Repeat([]byte{fill}, 32))
}

func testEnvelope(subscriptionID string) fcm.Envelope {
	return fcm.Envelope{
		Type:           "notification",
		V:              1,
		SubscriptionID: subscriptionID,
		Nonce:          protocol.BytesToBase64URL(bytes.Repeat([]byte{1}, 12)),
		Ciphertext:     protocol.BytesToBase64URL([]byte("encrypted payload")),
	}
}

func newTestRelay(t *testing.T, statePath string, now time.Time, dailyLimit, perSubscriptionLimit int, sender *recordingSender) (*httptest.Server, *Store) {
	t.Helper()
	store, err := OpenStore(statePath, dailyLimit, perSubscriptionLimit)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	server, err := NewServer(ServerOptions{Store: store, Sender: sender, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return httptest.NewServer(server.Handler()), store
}

func newTestClient(t *testing.T, serverURL string, now time.Time) *Client {
	t.Helper()
	client, err := NewClient(serverURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.Now = func() time.Time { return now }
	return client
}

func registerTestToken(t *testing.T, registrationURL, subscriptionID, token string) int {
	t.Helper()
	body, err := json.Marshal(RegistrationRequest{
		SubscriptionID: subscriptionID,
		Provider:       "fcm",
		PushToken:      token,
		Platform:       "android",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(registrationURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register token: %v", err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func provisionAndRegister(t *testing.T, client *Client, subscriptionID, key, token string) ProvisionResponse {
	t.Helper()
	provisioned, err := client.Provision(context.Background(), subscriptionID, key)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if status := registerTestToken(t, provisioned.RegistrationURL, subscriptionID, token); status != http.StatusOK {
		t.Fatalf("register returned HTTP %d", status)
	}
	return provisioned
}

func TestRelayPairSendLimitsAndPersistence(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	statePath := filepath.Join(t.TempDir(), "state.json")
	sender := &recordingSender{}
	server, _ := newTestRelay(t, statePath, now, 3, 2, sender)
	client := newTestClient(t, server.URL, now)

	subscriptionID := uuid.Must(uuid.NewRandom()).String()
	key := testKey(7)
	provisioned := provisionAndRegister(t, client, subscriptionID, key, "phone-one")
	if status := registerTestToken(t, strings.Replace(provisioned.RegistrationURL, "token=", "token=wrong", 1), subscriptionID, "attacker"); status != http.StatusUnauthorized {
		t.Fatalf("bad registration token returned HTTP %d", status)
	}

	status, err := client.Status(context.Background(), subscriptionID, key)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.RegisteredTokens != 1 {
		t.Fatalf("registered token count = %d, want 1", status.RegisteredTokens)
	}

	for expectedRemaining := 1; expectedRemaining >= 0; expectedRemaining-- {
		result, err := client.Send(context.Background(), subscriptionID, key, "test", testEnvelope(subscriptionID), nil)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		if result.Sent != 1 || result.SubscriptionDailyRemaining != expectedRemaining {
			t.Fatalf("unexpected send result: %+v", result)
		}
	}
	_, err = client.Send(context.Background(), subscriptionID, key, "test", testEnvelope(subscriptionID), nil)
	var httpError *HTTPError
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusTooManyRequests || !strings.Contains(httpError.Message, "subscription") {
		t.Fatalf("expected per-subscription HTTP 429, got %v", err)
	}

	secondID := uuid.Must(uuid.NewRandom()).String()
	secondKey := testKey(9)
	provisionAndRegister(t, client, secondID, secondKey, "phone-two")
	if _, err := client.Send(context.Background(), secondID, secondKey, "test", testEnvelope(secondID), nil); err != nil {
		t.Fatalf("third global delivery: %v", err)
	}
	_, err = client.Send(context.Background(), secondID, secondKey, "test", testEnvelope(secondID), nil)
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusTooManyRequests || !strings.Contains(httpError.Message, "global") {
		t.Fatalf("expected global HTTP 429, got %v", err)
	}
	server.Close()

	restarted, _ := newTestRelay(t, statePath, now, 3, 2, sender)
	defer restarted.Close()
	restartedClient := newTestClient(t, restarted.URL, now)
	_, err = restartedClient.Send(context.Background(), secondID, secondKey, "test", testEnvelope(secondID), nil)
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected persisted limit after restart, got %v", err)
	}
	restarted.Close()

	nextDay := now.Add(24 * time.Hour)
	nextDayServer, _ := newTestRelay(t, statePath, nextDay, 3, 2, sender)
	defer nextDayServer.Close()
	nextDayClient := newTestClient(t, nextDayServer.URL, nextDay)
	if _, err := nextDayClient.Send(context.Background(), secondID, secondKey, "test", testEnvelope(secondID), nil); err != nil {
		t.Fatalf("send after UTC-day reset: %v", err)
	}

	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stateBytes, []byte(key)) || bytes.Contains(stateBytes, []byte(secondKey)) {
		t.Fatal("relay state contains a subscription encryption key")
	}
}

func TestRelayRejectsReplayWrongKeyAndKeyConflict(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	statePath := filepath.Join(t.TempDir(), "state.json")
	sender := &recordingSender{}
	server, _ := newTestRelay(t, statePath, now, 10, 10, sender)
	defer server.Close()
	client := newTestClient(t, server.URL, now)

	subscriptionID := uuid.Must(uuid.NewRandom()).String()
	key := testKey(3)
	publicKey, err := PublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	unsignedBody, err := json.Marshal(ProvisionRequest{SubscriptionID: subscriptionID, PublicKey: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	unsignedResponse, err := http.Post(server.URL+"/v1/subscriptions", "application/json", bytes.NewReader(unsignedBody))
	if err != nil {
		t.Fatal(err)
	}
	unsignedResponse.Body.Close()
	if unsignedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned provision returned HTTP %d, want 401", unsignedResponse.StatusCode)
	}

	provisionAndRegister(t, client, subscriptionID, key, "phone")
	if _, err := client.Provision(context.Background(), subscriptionID, testKey(4)); err == nil {
		t.Fatal("expected key-binding conflict")
	}

	requestBody, err := json.Marshal(SendRequest{
		SubscriptionID: subscriptionID,
		Service:        "test",
		Envelope:       testEnvelope(subscriptionID),
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/send", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if err := SignRequest(request, requestBody, subscriptionID, key, now); err != nil {
		t.Fatal(err)
	}
	headers := request.Header.Clone()

	for attempt, expected := range []int{http.StatusOK, http.StatusConflict} {
		replay, err := http.NewRequest(http.MethodPost, server.URL+"/v1/send", bytes.NewReader(requestBody))
		if err != nil {
			t.Fatal(err)
		}
		replay.Header = headers.Clone()
		response, err := http.DefaultClient.Do(replay)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != expected {
			t.Fatalf("attempt %d returned HTTP %d, want %d", attempt+1, response.StatusCode, expected)
		}
	}

	wrongKeyClient := newTestClient(t, server.URL, now)
	_, err = wrongKeyClient.Send(context.Background(), subscriptionID, testKey(8), "test", testEnvelope(subscriptionID), nil)
	var httpError *HTTPError
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for wrong key, got %v", err)
	}
}

// errorsAs keeps the assertions readable without shadowing the package name in tests.
func errorsAs(err error, target interface{}) bool {
	return errors.As(err, target)
}
