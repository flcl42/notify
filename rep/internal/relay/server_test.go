package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	return newTestRelayWithIPLimit(t, statePath, now, dailyLimit, perSubscriptionLimit, 10, sender)
}

func newTestRelayWithIPLimit(t *testing.T, statePath string, now time.Time, dailyLimit, perSubscriptionLimit, perIPSubscriptionLimit int, sender *recordingSender) (*httptest.Server, *Store) {
	t.Helper()
	store, err := OpenStore(statePath, dailyLimit, perSubscriptionLimit, perIPSubscriptionLimit)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	server, err := NewServer(ServerOptions{Store: store, Sender: sender, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return httptest.NewServer(server.Handler()), store
}

func TestRelayLimitsDistinctSubscriptionsPerSourceIP(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	statePath := filepath.Join(t.TempDir(), "state.json")
	sender := &recordingSender{}
	server, _ := newTestRelayWithIPLimit(t, statePath, now, 100, 100, 2, sender)
	client := newTestClient(t, server.URL, now)
	unusedID := uuid.Must(uuid.NewRandom()).String()
	unusedKey := testKey(9)
	if _, err := client.Provision(context.Background(), unusedID, unusedKey); err != nil {
		t.Fatalf("provision unused subscription: %v", err)
	}
	_, err := client.Send(context.Background(), unusedID, unusedKey, "test", testEnvelope(unusedID), nil)
	var noTokenError *HTTPError
	if !errorsAs(err, &noTokenError) || noTokenError.StatusCode != http.StatusConflict {
		t.Fatalf("expected no-token HTTP 409, got %v", err)
	}

	type testSubscription struct {
		id  string
		key string
	}
	subscriptions := make([]testSubscription, 3)
	for i := range subscriptions {
		subscriptions[i] = testSubscription{
			id:  uuid.Must(uuid.NewRandom()).String(),
			key: testKey(byte(i + 1)),
		}
		provisionAndRegister(t, client, subscriptions[i].id, subscriptions[i].key, fmt.Sprintf("phone-%d", i))
	}

	for i := 0; i < 2; i++ {
		if _, err := client.Send(context.Background(), subscriptions[i].id, subscriptions[i].key, "test", testEnvelope(subscriptions[i].id), nil); err != nil {
			t.Fatalf("send for subscription %d: %v", i, err)
		}
	}
	if _, err := client.Send(context.Background(), subscriptions[0].id, subscriptions[0].key, "test", testEnvelope(subscriptions[0].id), nil); err != nil {
		t.Fatalf("repeat send for an already-counted subscription: %v", err)
	}

	_, err = client.Send(context.Background(), subscriptions[2].id, subscriptions[2].key, "test", testEnvelope(subscriptions[2].id), nil)
	var httpError *HTTPError
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusTooManyRequests || !strings.Contains(httpError.Message, "source IP") {
		t.Fatalf("expected source-IP HTTP 429, got %v", err)
	}
	server.Close()

	restarted, _ := newTestRelayWithIPLimit(t, statePath, now, 100, 100, 2, sender)
	restartedClient := newTestClient(t, restarted.URL, now)
	_, err = restartedClient.Send(context.Background(), subscriptions[2].id, subscriptions[2].key, "test", testEnvelope(subscriptions[2].id), nil)
	if !errorsAs(err, &httpError) || httpError.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected persisted source-IP limit after restart, got %v", err)
	}
	restarted.Close()

	nextDay := now.Add(24 * time.Hour)
	nextDayServer, _ := newTestRelayWithIPLimit(t, statePath, nextDay, 100, 100, 2, sender)
	defer nextDayServer.Close()
	nextDayClient := newTestClient(t, nextDayServer.URL, nextDay)
	if _, err := nextDayClient.Send(context.Background(), subscriptions[2].id, subscriptions[2].key, "test", testEnvelope(subscriptions[2].id), nil); err != nil {
		t.Fatalf("send after source-IP UTC-day reset: %v", err)
	}
}

func TestRequestSourceIDUsesOnlyTrustedForwardingHeaders(t *testing.T) {
	trusted, err := parseTrustedProxyCIDRs([]string{"172.17.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{trustedProxies: trusted}

	proxied := httptest.NewRequest(http.MethodPost, "http://relay.test/v1/send", nil)
	proxied.RemoteAddr = "172.17.0.2:12345"
	proxied.Header.Set("X-Forwarded-For", "192.0.2.10, 198.51.100.20")
	proxiedID, err := server.requestSourceID(proxied)
	if err != nil {
		t.Fatal(err)
	}

	rightmostForwarded := httptest.NewRequest(http.MethodPost, "http://relay.test/v1/send", nil)
	rightmostForwarded.RemoteAddr = "198.51.100.20:443"
	rightmostID, err := server.requestSourceID(rightmostForwarded)
	if err != nil {
		t.Fatal(err)
	}
	if proxiedID != rightmostID {
		t.Fatal("trusted proxy did not select the rightmost untrusted forwarded address")
	}

	direct := httptest.NewRequest(http.MethodPost, "http://relay.test/v1/send", nil)
	direct.RemoteAddr = "203.0.113.30:443"
	direct.Header.Set("X-Forwarded-For", "198.51.100.20")
	directID, err := server.requestSourceID(direct)
	if err != nil {
		t.Fatal(err)
	}
	if directID == rightmostID {
		t.Fatal("untrusted peer was able to spoof X-Forwarded-For")
	}

	v6a := httptest.NewRequest(http.MethodPost, "http://relay.test/v1/send", nil)
	v6a.RemoteAddr = "[2001:db8:1234:5678::1]:443"
	v6b := httptest.NewRequest(http.MethodPost, "http://relay.test/v1/send", nil)
	v6b.RemoteAddr = "[2001:db8:1234:5678::ffff]:443"
	v6aID, _ := server.requestSourceID(v6a)
	v6bID, _ := server.requestSourceID(v6b)
	if v6aID != v6bID {
		t.Fatal("IPv6 addresses in one /64 did not share a quota identity")
	}
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

func TestBrowserPairingURLKeepsCredentialOutOfHTTPRequest(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.json"), 10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	relayServer, err := NewServer(ServerOptions{Store: store, Sender: &recordingSender{}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	requestURI := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURI = r.RequestURI
		relayServer.Handler().ServeHTTP(w, r)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, now)

	key := testKey(5)
	applicationURL, err := protocol.CreatePairingURL(protocol.Subscription{
		ID:           uuid.Must(uuid.NewRandom()).String(),
		Name:         "System Scanner",
		DefaultTitle: "System Scanner",
		Key:          key,
	}, "http://registration.invalid/register")
	if err != nil {
		t.Fatal(err)
	}
	browserURL, err := client.BrowserPairingURL(applicationURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(browserURL, server.URL+"/pair#payload=") {
		t.Fatalf("unexpected browser pairing URL: %s", browserURL)
	}

	response, err := http.Get(browserURL)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if requestURI != "/pair" {
		t.Fatalf("pairing credential reached HTTP server in request URI %q", requestURI)
	}
	if bytes.Contains(page, []byte(key)) || bytes.Contains(page, []byte(strings.TrimPrefix(browserURL, server.URL+"/pair#payload="))) {
		t.Fatal("pairing page contains the QR credential")
	}
	if !bytes.Contains(page, []byte("intent://pair")) || !bytes.Contains(page, []byte("package=dev.privatenotify")) {
		t.Fatal("pairing page does not launch the Android package")
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing private pairing response headers: %v", response.Header)
	}
}

func TestRelayPublishesAndroidAppLinkAssociation(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	server, _ := newTestRelay(t, filepath.Join(t.TempDir(), "state.json"), now, 10, 10, &recordingSender{})
	defer server.Close()

	response, err := http.Get(server.URL + "/.well-known/assetlinks.json")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("asset links returned HTTP %d", response.StatusCode)
	}
	if response.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", response.Header.Get("Content-Type"))
	}

	var statements []struct {
		Relation []string `json:"relation"`
		Target   struct {
			Namespace    string   `json:"namespace"`
			PackageName  string   `json:"package_name"`
			Fingerprints []string `json:"sha256_cert_fingerprints"`
		} `json:"target"`
	}
	if err := json.NewDecoder(response.Body).Decode(&statements); err != nil {
		t.Fatalf("decode asset links: %v", err)
	}
	if len(statements) != 1 || len(statements[0].Relation) != 1 || statements[0].Relation[0] != "delegate_permission/common.handle_all_urls" {
		t.Fatalf("unexpected app-link relations: %+v", statements)
	}
	target := statements[0].Target
	if target.Namespace != "android_app" || target.PackageName != "dev.privatenotify" || len(target.Fingerprints) != 1 || target.Fingerprints[0] != "22:15:50:E1:02:FE:B2:67:00:B4:51:6D:EE:E7:D4:18:98:03:BF:09:81:71:96:84:00:92:79:85:AA:3C:A8:DD" {
		t.Fatalf("unexpected app-link target: %+v", target)
	}
}

// errorsAs keeps the assertions readable without shadowing the package name in tests.
func errorsAs(err error, target interface{}) bool {
	return errors.As(err, target)
}
