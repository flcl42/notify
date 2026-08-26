package relay

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flcl42/notify/rep/internal/protocol"
)

const (
	HeaderSubscription = "X-Notify-Subscription"
	HeaderTimestamp    = "X-Notify-Timestamp"
	HeaderNonce        = "X-Notify-Nonce"
	HeaderSignature    = "X-Notify-Signature"

	signatureContext = "private-notify-relay-signing-v1"
)

func signingPrivateKey(subscriptionKey string) (ed25519.PrivateKey, error) {
	key, err := protocol.Base64URLToBytes(subscriptionKey)
	if err != nil {
		return nil, fmt.Errorf("invalid subscription key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid subscription key length: got %d, want 32", len(key))
	}
	domainSeparated := append([]byte(signatureContext+"\x00"), key...)
	seed := sha256.Sum256(domainSeparated)
	return ed25519.NewKeyFromSeed(seed[:]), nil
}

func PublicKey(subscriptionKey string) (string, error) {
	privateKey, err := signingPrivateKey(subscriptionKey)
	if err != nil {
		return "", err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return base64.RawURLEncoding.EncodeToString(publicKey), nil
}

func canonicalRequest(method, path, subscriptionID, timestamp, nonce string, body []byte) []byte {
	digest := sha256.Sum256(body)
	return []byte(strings.Join([]string{
		"PRIVATE-NOTIFY-RELAY-V1",
		strings.ToUpper(method),
		path,
		subscriptionID,
		timestamp,
		nonce,
		hex.EncodeToString(digest[:]),
	}, "\n"))
}

func SignRequest(req *http.Request, body []byte, subscriptionID, subscriptionKey string, now time.Time) error {
	privateKey, err := signingPrivateKey(subscriptionKey)
	if err != nil {
		return err
	}

	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return err
	}
	timestamp := strconv.FormatInt(now.UTC().Unix(), 10)
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	signature := ed25519.Sign(privateKey, canonicalRequest(req.Method, path, subscriptionID, timestamp, nonce, body))

	req.Header.Set(HeaderSubscription, subscriptionID)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderNonce, nonce)
	req.Header.Set(HeaderSignature, base64.RawURLEncoding.EncodeToString(signature))
	return nil
}

type VerifiedRequest struct {
	SubscriptionID string
	Nonce          string
	Timestamp      time.Time
}

func VerifyRequest(req *http.Request, body []byte, expectedSubscriptionID, encodedPublicKey string, now time.Time) (VerifiedRequest, error) {
	subscriptionID := req.Header.Get(HeaderSubscription)
	timestampText := req.Header.Get(HeaderTimestamp)
	nonce := req.Header.Get(HeaderNonce)
	signatureText := req.Header.Get(HeaderSignature)
	if subscriptionID == "" || timestampText == "" || nonce == "" || signatureText == "" {
		return VerifiedRequest{}, fmt.Errorf("missing relay authentication headers")
	}
	if expectedSubscriptionID != "" && subtle.ConstantTimeCompare([]byte(subscriptionID), []byte(expectedSubscriptionID)) != 1 {
		return VerifiedRequest{}, fmt.Errorf("subscription id mismatch")
	}

	timestampUnix, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		return VerifiedRequest{}, fmt.Errorf("invalid authentication timestamp")
	}
	timestamp := time.Unix(timestampUnix, 0).UTC()
	if delta := now.UTC().Sub(timestamp); delta < -5*time.Minute || delta > 5*time.Minute {
		return VerifiedRequest{}, fmt.Errorf("authentication timestamp is outside the allowed window")
	}
	if _, err := base64.RawURLEncoding.DecodeString(nonce); err != nil || len(nonce) > 128 {
		return VerifiedRequest{}, fmt.Errorf("invalid authentication nonce")
	}

	publicKey, err := base64.RawURLEncoding.DecodeString(encodedPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return VerifiedRequest{}, fmt.Errorf("invalid stored public key")
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return VerifiedRequest{}, fmt.Errorf("invalid request signature")
	}
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if !ed25519.Verify(publicKey, canonicalRequest(req.Method, path, subscriptionID, timestampText, nonce, body), signature) {
		return VerifiedRequest{}, fmt.Errorf("invalid request signature")
	}

	return VerifiedRequest{SubscriptionID: subscriptionID, Nonce: nonce, Timestamp: timestamp}, nil
}
