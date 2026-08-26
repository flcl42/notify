package relay

import "github.com/flcl42/notify/rep/internal/fcm"

type ProvisionRequest struct {
	SubscriptionID string `json:"subscriptionId"`
	PublicKey      string `json:"publicKey"`
}

type ProvisionResponse struct {
	RegistrationURL string `json:"registrationUrl"`
}

type RegistrationRequest struct {
	SubscriptionID string `json:"subscriptionId"`
	Provider       string `json:"provider"`
	PushToken      string `json:"pushToken"`
	Platform       string `json:"platform"`
}

type StatusResponse struct {
	RegisteredTokens int `json:"registeredTokens"`
}

type SendRequest struct {
	SubscriptionID string          `json:"subscriptionId"`
	Service        string          `json:"service"`
	Envelope       fcm.Envelope    `json:"envelope"`
	PushTokens     []fcm.PushToken `json:"pushTokens,omitempty"`
}

type SendResponse struct {
	Sent                       int `json:"sent"`
	DailyRemaining             int `json:"dailyRemaining"`
	SubscriptionDailyRemaining int `json:"subscriptionDailyRemaining"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
