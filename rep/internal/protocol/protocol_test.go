package protocol

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBase64URLRoundTrip(t *testing.T) {
	original := []byte{0x00, 0x01, 0xff, 0xfe}
	encoded := BytesToBase64URL(original)
	decoded, err := Base64URLToBytes(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if string(decoded) != string(original) {
		t.Fatalf("decoded mismatch: %x != %x", decoded, original)
	}
}

func TestCreatePairingURL(t *testing.T) {
	sub := Subscription{
		ID:           uuid.Must(uuid.NewRandom()).String(),
		Title:        "Build Alerts",
		Name:         "Build Alerts",
		DefaultTitle: "Build Alerts",
		Key:          BytesToBase64URL(make([]byte, 32)),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	url, err := CreatePairingURL(sub, "http://127.0.0.1:8788/register")
	if err != nil {
		t.Fatalf("create pairing url: %v", err)
	}
	if !startsWith(url, PairingScheme+"://pair?payload=") {
		t.Fatalf("unexpected url: %s", url)
	}

	parsed, err := ParsePairingURL(url)
	if err != nil {
		t.Fatalf("parse pairing url: %v", err)
	}
	if parsed.DefaultTitle != "Build Alerts" {
		t.Fatalf("default title mismatch: %s", parsed.DefaultTitle)
	}
	if parsed.SubscriptionID != sub.ID {
		t.Fatalf("subscription id mismatch")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	sub := Subscription{
		ID:           uuid.Must(uuid.NewRandom()).String(),
		Title:        "Test Source",
		Name:         "Test Source",
		DefaultTitle: "Test Source",
		Key:          BytesToBase64URL(key),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}

	envelope, err := EncryptNotification(sub, Notification{
		V:              "1",
		ID:             uuid.Must(uuid.NewRandom()).String(),
		SubscriptionID: sub.ID,
		Service:        "test",
		Title:          "Native title",
		Body:           "Native body",
		Data:           map[string]interface{}{"priority": "normal"},
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := DecryptNotification(sub, *envelope)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted.SubscriptionID != sub.ID {
		t.Fatalf("subscription id mismatch")
	}
	if decrypted.Service != "test" {
		t.Fatalf("service mismatch")
	}
	if decrypted.Title != "Native title" {
		t.Fatalf("title mismatch: %s", decrypted.Title)
	}
	if decrypted.Body != "Native body" {
		t.Fatalf("body mismatch: %s", decrypted.Body)
	}
	if decrypted.Data["priority"] != "normal" {
		t.Fatalf("data mismatch")
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func ParsePairingURL(url string) (PairingPayload, error) {
	const prefix = PairingScheme + "://pair?payload="
	encoded := url[len(prefix):]
	var payload PairingPayload
	if err := DecodeJSONPayload(encoded, &payload); err != nil {
		return PairingPayload{}, err
	}
	return payload, nil
}
