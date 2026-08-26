package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flcl42/notify/rep/internal/fcm"
	"github.com/google/uuid"
)

type PushSender interface {
	SendPushNotifications(pushTokens []fcm.PushToken, envelope fcm.Envelope, service string) (fcm.SendResult, error)
}

type ServerOptions struct {
	Store     *Store
	Sender    PushSender
	PublicURL string
	Now       func() time.Time
	Logf      func(format string, args ...interface{})
}

type Server struct {
	store     *Store
	sender    PushSender
	publicURL string
	now       func() time.Time
	logf      func(format string, args ...interface{})
}

func NewServer(options ServerOptions) (*Server, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("relay store is required")
	}
	if options.Sender == nil {
		return nil, fmt.Errorf("FCM sender is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	logf := options.Logf
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	return &Server{
		store:     options.Store,
		sender:    options.Sender,
		publicURL: strings.TrimRight(strings.TrimSpace(options.PublicURL), "/"),
		now:       now,
		logf:      logf,
	}, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "time": s.now().UTC().Format(time.RFC3339)})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/subscriptions":
		s.handleProvision(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/subscriptions/") && strings.HasSuffix(r.URL.Path, "/status"):
		s.handleStatus(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/register/"):
		s.handleRegistration(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/send":
		s.handleSend(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var request ProvisionRequest
	if err := decodeJSONBytes(body, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := uuid.Parse(request.SubscriptionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscription id")
		return
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(request.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "invalid signing public key")
		return
	}
	if _, err := VerifyRequest(r, body, request.SubscriptionID, request.PublicKey, s.now()); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	registrationKey, err := s.store.Provision(request.SubscriptionID, request.PublicKey, s.now())
	if errors.Is(err, ErrPublicKeyConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not persist subscription")
		return
	}

	baseURL := s.publicURL
	if baseURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = scheme + "://" + r.Host
	}
	registrationURL := baseURL + "/v1/register/" + url.PathEscape(request.SubscriptionID) + "?token=" + url.QueryEscape(registrationKey)
	writeJSON(w, http.StatusOK, ProvisionResponse{RegistrationURL: registrationURL})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	subscriptionID, ok := subscriptionIDFromStatusPath(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	publicKey, err := s.store.PublicKey(subscriptionID)
	if errors.Is(err, ErrUnknownSubscription) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load subscription")
		return
	}
	if _, err := VerifyRequest(r, nil, subscriptionID, publicKey, s.now()); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	count, err := s.store.RegisteredTokenCount(subscriptionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load registration status")
		return
	}
	writeJSON(w, http.StatusOK, StatusResponse{RegisteredTokens: count})
}

func (s *Server) handleRegistration(w http.ResponseWriter, r *http.Request) {
	subscriptionID, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/v1/register/"))
	if err != nil || subscriptionID == "" || strings.Contains(subscriptionID, "/") {
		writeError(w, http.StatusBadRequest, "invalid subscription id")
		return
	}
	var request RegistrationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.SubscriptionID != subscriptionID {
		writeError(w, http.StatusBadRequest, "subscription id mismatch")
		return
	}
	if request.Provider != "fcm" || strings.TrimSpace(request.PushToken) == "" || len(request.PushToken) > 16384 {
		writeError(w, http.StatusBadRequest, "invalid FCM registration")
		return
	}
	err = s.store.Register(subscriptionID, r.URL.Query().Get("token"), request, s.now())
	switch {
	case errors.Is(err, ErrUnknownSubscription):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrRegistrationDenied):
		writeError(w, http.StatusUnauthorized, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not persist push registration")
	default:
		s.logf("registered FCM token for subscription %s", subscriptionID)
		writeJSON(w, http.StatusOK, map[string]bool{"registered": true})
	}
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var request SendRequest
	if err := decodeJSONBytes(body, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := uuid.Parse(request.SubscriptionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid subscription id")
		return
	}
	if request.Envelope.Type != "notification" || request.Envelope.V != 1 || request.Envelope.SubscriptionID != request.SubscriptionID || request.Envelope.Nonce == "" || request.Envelope.Ciphertext == "" {
		writeError(w, http.StatusBadRequest, "invalid encrypted notification envelope")
		return
	}
	if len(request.Service) > 128 {
		writeError(w, http.StatusBadRequest, "service name is too long")
		return
	}
	publicKey, err := s.store.PublicKey(request.SubscriptionID)
	if errors.Is(err, ErrUnknownSubscription) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load subscription")
		return
	}
	verified, err := VerifyRequest(r, body, request.SubscriptionID, publicKey, s.now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	pushTokens, err := s.store.MergePushTokens(request.SubscriptionID, request.PushTokens)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load push targets")
		return
	}
	if len(pushTokens) == 0 {
		writeError(w, http.StatusConflict, "no FCM push tokens are registered for this subscription")
		return
	}
	limits, err := s.store.Reserve(request.SubscriptionID, verified.Nonce, len(pushTokens), s.now())
	var rateLimit *RateLimitError
	switch {
	case errors.Is(err, ErrReplay):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.As(err, &rateLimit):
		w.Header().Set("Retry-After", secondsUntilNextUTCDay(s.now()))
		writeError(w, http.StatusTooManyRequests, rateLimit.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not reserve notification quota")
		return
	}

	result, err := s.sender.SendPushNotifications(pushTokens, request.Envelope, request.Service)
	if err != nil {
		s.logf("FCM delivery failed for subscription %s: %v", request.SubscriptionID, err)
		writeError(w, http.StatusBadGateway, "FCM delivery failed")
		return
	}
	s.logf("sent %d notification delivery(s) for subscription %s", result.Sent, request.SubscriptionID)
	writeJSON(w, http.StatusOK, SendResponse{
		Sent:                       result.Sent,
		DailyRemaining:             limits.DailyRemaining,
		SubscriptionDailyRemaining: limits.SubscriptionDailyRemaining,
	})
}

func subscriptionIDFromStatusPath(path string) (string, bool) {
	const prefix = "/v1/subscriptions/"
	const suffix = "/status"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	value, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix))
	if err != nil || value == "" || strings.Contains(value, "/") {
		return "", false
	}
	return value, true
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("request body is too large or unreadable")
	}
	return body, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) error {
	body, err := readBody(w, r)
	if err != nil {
		return err
	}
	return decodeJSONBytes(body, target)
}

func decodeJSONBytes(body []byte, target interface{}) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid JSON request")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

func secondsUntilNextUTCDay(now time.Time) string {
	utc := now.UTC()
	next := time.Date(utc.Year(), utc.Month(), utc.Day()+1, 0, 0, 0, 0, time.UTC)
	seconds := int(next.Sub(utc).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d", seconds)
}
