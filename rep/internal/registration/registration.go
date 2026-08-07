package registration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type Registration struct {
	Provider     string `json:"provider"`
	Token        string `json:"token"`
	Platform     string `json:"platform"`
	RegisteredAt string `json:"registeredAt"`
}

type RegisterFunc func(Registration) error

type Server struct {
	SubscriptionID string
	Port           int
	Host           string
	OnRegister     RegisterFunc

	registered chan struct{}
	subscription Registration
	httpServer   *http.Server
	listener     net.Listener
}

func New(subscriptionID string, port int, host string, onRegister RegisterFunc) *Server {
	if onRegister == nil {
		onRegister = func(Registration) error { return nil }
	}
	return &Server{
		SubscriptionID: subscriptionID,
		Port:           port,
		Host:           host,
		OnRegister:     onRegister,
		registered:     make(chan struct{}),
	}
}

func (s *Server) Registered() <-chan Registration {
	ch := make(chan Registration)
	go func() {
		<-s.registered
		ch <- s.subscription
		close(ch)
	}()
	return ch
}

func sendJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if r.Method == http.MethodGet && path == "/health" {
		sendJSON(w, http.StatusOK, map[string]interface{}{
			"ok":             true,
			"subscriptionId": s.SubscriptionID,
		})
		return
	}

	if r.Method != http.MethodPost || path != "/register" {
		sendJSON(w, http.StatusNotFound, map[string]interface{}{
			"ok":    false,
			"error": "Not found.",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	var payload map[string]interface{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
	}

	subID, _ := payload["subscriptionId"].(string)
	if subID != s.SubscriptionID {
		sendJSON(w, http.StatusForbidden, map[string]interface{}{
			"ok":    false,
			"error": "Subscription mismatch.",
		})
		return
	}

	token := ""
	if v, ok := payload["pushToken"].(string); ok && v != "" {
		token = v
	} else if v, ok := payload["token"].(string); ok && v != "" {
		token = v
	}
	if token == "" {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok":    false,
			"error": "Missing push token.",
		})
		return
	}

	provider := "fcm"
	if v, ok := payload["provider"].(string); ok && v != "" {
		provider = v
	}
	platform := "unknown"
	if v, ok := payload["platform"].(string); ok && v != "" {
		platform = v
	}

	reg := Registration{
		Provider:     provider,
		Token:        token,
		Platform:     platform,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := s.OnRegister(reg); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	s.subscription = reg
	close(s.registered)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"subscriptionId": s.SubscriptionID,
		"pushTokenCount": 1,
	})
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = listener
	s.httpServer = &http.Server{Handler: s}
	go func() {
		_ = s.httpServer.Serve(listener)
	}()
	return nil
}

func (s *Server) Address() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) Close() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
