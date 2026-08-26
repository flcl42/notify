package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flcl42/notify/rep/internal/fcm"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("relay returned HTTP %d: %s", e.StatusCode, e.Message)
}

func NewClient(baseURL string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid relay server URL %q", baseURL)
	}
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
		Now:        time.Now,
	}, nil
}

func (c *Client) Provision(ctx context.Context, subscriptionID, subscriptionKey string) (ProvisionResponse, error) {
	publicKey, err := PublicKey(subscriptionKey)
	if err != nil {
		return ProvisionResponse{}, err
	}
	request := ProvisionRequest{SubscriptionID: subscriptionID, PublicKey: publicKey}
	var response ProvisionResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/subscriptions", request, &response, subscriptionID, subscriptionKey); err != nil {
		return ProvisionResponse{}, err
	}
	if response.RegistrationURL == "" {
		return ProvisionResponse{}, fmt.Errorf("relay returned an empty registration URL")
	}
	return response, nil
}

func (c *Client) Status(ctx context.Context, subscriptionID, subscriptionKey string) (StatusResponse, error) {
	var response StatusResponse
	path := "/v1/subscriptions/" + url.PathEscape(subscriptionID) + "/status"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response, subscriptionID, subscriptionKey); err != nil {
		return StatusResponse{}, err
	}
	return response, nil
}

func (c *Client) Send(ctx context.Context, subscriptionID, subscriptionKey, service string, envelope fcm.Envelope, pushTokens []fcm.PushToken) (SendResponse, error) {
	request := SendRequest{
		SubscriptionID: subscriptionID,
		Service:        service,
		Envelope:       envelope,
		PushTokens:     pushTokens,
	}
	var response SendResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/send", request, &response, subscriptionID, subscriptionKey); err != nil {
		return SendResponse{}, err
	}
	return response, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestValue, responseValue interface{}, subscriptionID, subscriptionKey string) error {
	var body []byte
	var err error
	if requestValue != nil {
		body, err = json.Marshal(requestValue)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if requestValue != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if subscriptionID != "" {
		if err := SignRequest(req, body, subscriptionID, subscriptionKey, c.Now()); err != nil {
			return err
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("relay request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		var apiError ErrorResponse
		if json.Unmarshal(responseBody, &apiError) == nil && apiError.Error != "" {
			message = apiError.Error
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return &HTTPError{StatusCode: resp.StatusCode, Message: message}
	}
	if responseValue == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, responseValue); err != nil {
		return fmt.Errorf("decode relay response: %w", err)
	}
	return nil
}
