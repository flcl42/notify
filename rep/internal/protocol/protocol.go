package protocol

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

const PairingScheme = "dev.privatenotify"

// Subscription mirrors the fields used for pairing and encryption.
type Subscription struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	DefaultTitle string `json:"defaultTitle"`
	Key          string `json:"key"`
	Delivery     string `json:"delivery"`
	CreatedAt    string `json:"createdAt"`
}

// PushToken is a registered push target.
type PushToken struct {
	Provider     string `json:"provider"`
	Token        string `json:"token"`
	Platform     string `json:"platform"`
	RegisteredAt string `json:"registeredAt"`
}

// Envelope is the encrypted notification payload.
type Envelope struct {
	Type           string `json:"type"`
	V              int    `json:"v"`
	SubscriptionID string `json:"subscriptionId"`
	Nonce          string `json:"nonce"`
	Ciphertext     string `json:"ciphertext"`
}

// PairingPayload is the JSON embedded in a pairing QR URL.
type PairingPayload struct {
	V              string `json:"v"`
	Type           string `json:"type"`
	SubscriptionID string `json:"subscriptionId"`
	Name           string `json:"name"`
	DefaultTitle   string `json:"defaultTitle"`
	Delivery       string `json:"delivery"`
	RegistrationURL string `json:"registrationUrl"`
	Key            string `json:"key"`
	CreatedAt      string `json:"createdAt"`
}

// Notification is the cleartext notification.
type Notification struct {
	V              string                 `json:"v"`
	ID             string                 `json:"id"`
	SubscriptionID string                 `json:"subscriptionId"`
	Service        string                 `json:"service"`
	Title          string                 `json:"title"`
	Body           string                 `json:"body"`
	Data           map[string]interface{} `json:"data"`
	CreatedAt      string                 `json:"createdAt"`
}

func BytesToBase64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func Base64URLToBytes(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func EncodeJSONPayload(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return BytesToBase64URL(b), nil
}

func DecodeJSONPayload(s string, v interface{}) error {
	b, err := Base64URLToBytes(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func CreatePairingURL(sub Subscription, registrationURL string) (string, error) {
	payload := PairingPayload{
		V:               "1",
		Type:            "notify-pairing",
		SubscriptionID:  sub.ID,
		Name:            sub.Name,
		DefaultTitle:    sub.DefaultTitle,
		Delivery:        "push",
		RegistrationURL: registrationURL,
		Key:             sub.Key,
		CreatedAt:       sub.CreatedAt,
	}
	encoded, err := EncodeJSONPayload(payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://pair?payload=%s", PairingScheme, encoded), nil
}

func EncryptNotification(sub Subscription, note Notification) (*Envelope, error) {
	key, err := Base64URLToBytes(sub.Key)
	if err != nil {
		return nil, fmt.Errorf("invalid subscription key: %w", err)
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	plaintext, err := json.Marshal(note)
	if err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)

	return &Envelope{
		Type:           "notification",
		V:              1,
		SubscriptionID: sub.ID,
		Nonce:          BytesToBase64URL(nonce),
		Ciphertext:     BytesToBase64URL(ciphertext),
	}, nil
}

func DecryptNotification(sub Subscription, env Envelope) (*Notification, error) {
	key, err := Base64URLToBytes(sub.Key)
	if err != nil {
		return nil, fmt.Errorf("invalid subscription key: %w", err)
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	nonce, err := Base64URLToBytes(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce: %w", err)
	}
	ciphertext, err := Base64URLToBytes(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("invalid ciphertext: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed: %w", err)
	}

	var note Notification
	if err := json.Unmarshal(plaintext, &note); err != nil {
		return nil, err
	}
	return &note, nil
}
