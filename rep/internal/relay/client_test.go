package relay

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/flcl42/notify/rep/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHostedRelayBypassesEnvironmentProxy(t *testing.T) {
	for _, serverURL := range []string{config.DefaultServerURL, "http://62.171.163.96:17891"} {
		client, err := NewClient(serverURL)
		if err != nil {
			t.Fatal(err)
		}
		transport, ok := client.HTTPClient.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil {
			t.Fatalf("hosted relay %q did not disable proxying", serverURL)
		}
	}

	custom, err := NewClient("https://relay.example")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := custom.HTTPClient.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("custom relay did not retain environment proxy support")
	}
}

func TestSendTransportErrorWarnsThatDeliveryIsAmbiguous(t *testing.T) {
	client, err := NewClient("https://relay.example")
	if err != nil {
		t.Fatal(err)
	}
	client.HTTPClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("response connection closed")
	})

	err = client.doJSON(context.Background(), http.MethodPost, "/v1/send", map[string]string{"test": "value"}, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "notification may already have been delivered") {
		t.Fatalf("unexpected transport error: %v", err)
	}
}
