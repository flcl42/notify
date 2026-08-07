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

type Envelope struct {
	SubscriptionID string `json:"subscriptionId"`
}

type SendOptions struct {
	Service         string
	ServiceAccountPath string
	ProjectID       string
	TokenURL        string
	URL             string
	TTL             string
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

func signJWT(sa *ServiceAccount) (string, error) {
	now := time.Now().Unix()
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	claims := map[string]interface{}{
		"iss": sa.ClientEmail,
		"scope": fcmScope,
		"aud": tokenURL,
		"iat": now,
		"exp": now + 3600,
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

func getAccessToken(sa *ServiceAccount, tokenURL string) (string, error) {
	assertion, err := signJWT(sa)
	if err != nil {
		return "", err
	}

	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	data.Set("assertion", assertion)

	resp, err := http.Post(tokenURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("could not parse token response: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not get Google access token: %s", string(body))
	}

	token, ok := payload["access_token"].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("could not get Google access token: %s", string(body))
	}
	return token, nil
}

func SendPushNotifications(pushTokens []PushToken, envelope Envelope, options SendOptions) (SendResult, error) {
	var fcmTokens []PushToken
	for _, t := range pushTokens {
		if t.Provider == "fcm" && t.Token != "" {
			fcmTokens = append(fcmTokens, t)
		}
	}

	if len(fcmTokens) == 0 {
		return SendResult{Sent: 0, Responses: []interface{}{}}, nil
	}

	serviceAccount, err := loadServiceAccount(options.ServiceAccountPath)
	if err != nil {
		return SendResult{}, err
	}

	projectID := options.ProjectID
	if projectID == "" {
		projectID = serviceAccount.ProjectID
	}
	if projectID == "" {
		return SendResult{}, fmt.Errorf("FCM project id is missing")
	}

	tokURL := options.TokenURL
	if tokURL == "" {
		tokURL = tokenURL
	}
	accessToken, err := getAccessToken(serviceAccount, tokURL)
	if err != nil {
		return SendResult{}, err
	}

	endpoint := options.URL
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", url.PathEscape(projectID))
	}

	ttl := options.TTL
	if ttl == "" {
		ttl = "3600s"
	}

	service := options.Service
	if service == "" {
		service = "rep"
	}

	result := SendResult{
		Sent:      len(fcmTokens),
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
					"ttl":      ttl,
				},
			},
		}

		body, err := json.Marshal(message)
		if err != nil {
			return SendResult{}, err
		}

		req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
		if err != nil {
			return SendResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
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
	}

	return result, nil
}
