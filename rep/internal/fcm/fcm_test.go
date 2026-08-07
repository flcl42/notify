package fcm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func generateServiceAccount(t *testing.T, dir string) string {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	sa := map[string]interface{}{
		"type":         "service_account",
		"project_id":   "test-project",
		"private_key":  string(pemBytes),
		"client_email": "test@test-project.iam.gserviceaccount.com",
	}
	path := filepath.Join(dir, "service-account.json")
	data, _ := json.Marshal(sa)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write service account: %v", err)
	}
	return path
}

func TestSendPushNotifications(t *testing.T) {
	dir := t.TempDir()
	serviceAccountPath := generateServiceAccount(t, dir)

	var requests []struct {
		URL  string
		Body json.RawMessage
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		_ = r.Body.Close()
		requests = append(requests, struct {
			URL  string
			Body json.RawMessage
		}{URL: r.URL.Path, Body: body})

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"test-token","expires_in":3600,"token_type":"Bearer"}`))
			return
		}
		w.Write([]byte(`{"name":"projects/test/messages/1"}`))
	}))
	defer server.Close()

	envelope := Envelope{SubscriptionID: uuid.Must(uuid.NewRandom()).String()}
	pushTokens := []PushToken{
		{Provider: "fcm", Token: "fcm-token"},
	}

	result, err := SendPushNotifications(pushTokens, envelope, SendOptions{
		Service:            "test",
		ServiceAccountPath: serviceAccountPath,
		TokenURL:           server.URL + "/token",
		URL:                server.URL + "/fcm",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if result.Sent != 1 {
		t.Fatalf("expected 1 sent, got %d", result.Sent)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}

	var fcmBody map[string]interface{}
	if err := json.Unmarshal(requests[1].Body, &fcmBody); err != nil {
		t.Fatalf("parse fcm body: %v", err)
	}
	message := fcmBody["message"].(map[string]interface{})
	if message["token"] != "fcm-token" {
		t.Fatalf("token mismatch")
	}
	android := message["android"].(map[string]interface{})
	if android["priority"] != "HIGH" {
		t.Fatalf("priority mismatch")
	}
	data := message["data"].(map[string]interface{})
	if data["encryptedEnvelope"] == "" {
		t.Fatalf("missing encrypted envelope")
	}
}
