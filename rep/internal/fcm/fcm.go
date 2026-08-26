package fcm

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	tokenURL = "https://oauth2.googleapis.com/token"
	fcmScope = "https://www.googleapis.com/auth/firebase.messaging"
)

type ServiceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
}

type PushToken struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
}

// Envelope mirrors protocol.Envelope. Every field is delivered to the phone as
// the encryptedEnvelope data value, which refuses to decrypt unless type, v,
// subscriptionId, nonce and ciphertext are all present.
type Envelope struct {
	Type           string `json:"type"`
	V              int    `json:"v"`
	SubscriptionID string `json:"subscriptionId"`
	Nonce          string `json:"nonce"`
	Ciphertext     string `json:"ciphertext"`
}

type SendOptions struct {
	Service            string
	ServiceAccountPath string
	ProjectID          string
	TokenURL           string
	URL                string
	TTL                string
	HTTPClient         *http.Client
}

type SendResult struct {
	Sent      int
	Responses []interface{}
}

func loadServiceAccount(path string) (*ServiceAccount, error) {
	if path == "" {
		return nil, fmt.Errorf("FCM send requires --fcm-service-account or GOOGLE_APPLICATION_CREDENTIALS")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sa ServiceAccount
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

func base64URLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return rsaKey, nil
}

func signJWT(sa *ServiceAccount, audience string) (string, error) {
	now := time.Now().Unix()
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	claims := map[string]interface{}{
		"iss":   sa.ClientEmail,
		"scope": fcmScope,
		"aud":   audience,
		"iat":   now,
		"exp":   now + 3600,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	input := base64URLEncode(headerJSON) + "." + base64URLEncode(claimsJSON)
	h := sha256.New()
	h.Write([]byte(input))
	digest := h.Sum(nil)

	privateKey, err := parsePrivateKey(sa.PrivateKey)
	if err != nil {
		return "", err
	}

	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest)
	if err != nil {
		return "", err
	}

	return input + "." + base64URLEncode(signature), nil
}

func getAccessToken(client *http.Client, sa *ServiceAccount, tokenURL string) (string, time.Duration, error) {
	assertion, err := signJWT(sa, tokenURL)
	if err != nil {
		return "", 0, err
	}

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", assertion)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", 0, fmt.Errorf("could not parse token response: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("could not get Google access token: %s", string(body))
	}

	if payload.AccessToken == "" {
		return "", 0, fmt.Errorf("could not get Google access token: %s", string(body))
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}
	return payload.AccessToken, time.Duration(payload.ExpiresIn) * time.Second, nil
}

type Sender struct {
	serviceAccount *ServiceAccount
	projectID      string
	tokenURL       string
	endpoint       string
	ttl            string
	httpClient     *http.Client

	mu                sync.Mutex
	cachedAccessToken string
	accessTokenExpiry time.Time
}

func NewSender(options SendOptions) (*Sender, error) {
	serviceAccount, err := loadServiceAccount(options.ServiceAccountPath)
	if err != nil {
		return nil, err
	}
	projectID := options.ProjectID
	if projectID == "" {
		projectID = serviceAccount.ProjectID
	}
	if projectID == "" {
		return nil, fmt.Errorf("FCM project id is missing")
	}
	tokURL := options.TokenURL
	if tokURL == "" {
		tokURL = tokenURL
	}
	endpoint := options.URL
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", url.PathEscape(projectID))
	}
	ttl := options.TTL
	if ttl == "" {
		ttl = "3600s"
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Sender{
		serviceAccount: serviceAccount,
		projectID:      projectID,
		tokenURL:       tokURL,
		endpoint:       endpoint,
		ttl:            ttl,
		httpClient:     httpClient,
	}, nil
}

func (s *Sender) accessToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedAccessToken != "" && time.Until(s.accessTokenExpiry) > time.Minute {
		return s.cachedAccessToken, nil
	}
	token, lifetime, err := getAccessToken(s.httpClient, s.serviceAccount, s.tokenURL)
	if err != nil {
		return "", err
	}
	s.cachedAccessToken = token
	s.accessTokenExpiry = time.Now().Add(lifetime)
	return token, nil
}

func (s *Sender) SendPushNotifications(pushTokens []PushToken, envelope Envelope, service string) (SendResult, error) {
	var fcmTokens []PushToken
	for _, t := range pushTokens {
		if t.Provider == "fcm" && t.Token != "" {
			fcmTokens = append(fcmTokens, t)
		}
	}

	if len(fcmTokens) == 0 {
		return SendResult{Sent: 0, Responses: []interface{}{}}, nil
	}

	accessToken, err := s.accessToken()
	if err != nil {
		return SendResult{}, err
	}
	if service == "" {
		service = "rep"
	}

	result := SendResult{
		Sent:      0,
		Responses: make([]interface{}, 0, len(fcmTokens)),
	}

	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return SendResult{}, err
	}

	for _, pushToken := range fcmTokens {
		message := map[string]interface{}{
			"message": map[string]interface{}{
				"token": pushToken.Token,
				"data": map[string]string{
					"encryptedEnvelope": string(envelopeJSON),
					"subscriptionId":    envelope.SubscriptionID,
					"service":           service,
				},
				"android": map[string]interface{}{
					"priority": "HIGH",
					"ttl":      s.ttl,
				},
			},
		}

		body, err := json.Marshal(message)
		if err != nil {
			return SendResult{}, err
		}

		req, err := http.NewRequest("POST", s.endpoint, bytes.NewReader(body))
		if err != nil {
			return SendResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return SendResult{}, err
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return SendResult{}, err
		}

		var parsed interface{}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			parsed = map[string]string{"raw": string(respBody)}
		}

		if resp.StatusCode != http.StatusOK {
			return SendResult{}, fmt.Errorf("FCM send failed with HTTP %d: %s", resp.StatusCode, string(respBody))
		}
		result.Responses = append(result.Responses, parsed)
		result.Sent++
	}

	return result, nil
}

func SendPushNotifications(pushTokens []PushToken, envelope Envelope, options SendOptions) (SendResult, error) {
	hasFCMToken := false
	for _, token := range pushTokens {
		if token.Provider == "fcm" && token.Token != "" {
			hasFCMToken = true
			break
		}
	}
	if !hasFCMToken {
		return SendResult{Sent: 0, Responses: []interface{}{}}, nil
	}
	sender, err := NewSender(options)
	if err != nil {
		return SendResult{}, err
	}
	return sender.SendPushNotifications(pushTokens, envelope, options.Service)
}
