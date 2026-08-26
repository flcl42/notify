package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flcl42/notify/rep/internal/config"
	"github.com/flcl42/notify/rep/internal/fcm"
	"github.com/flcl42/notify/rep/internal/relay"
	"github.com/flcl42/notify/rep/internal/version"
)

func main() {
	defaultCredential := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if defaultCredential == "" {
		defaultCredential = os.Getenv("FCM_SERVICE_ACCOUNT")
	}

	listen := flag.String("listen", ":17891", "HTTP listen address")
	publicURL := flag.String("public-url", config.DefaultServerURL, "public relay base URL embedded in pairing URLs")
	statePath := flag.String("state", "notify-state.json", "persistent relay state path")
	serviceAccount := flag.String("fcm-service-account", defaultCredential, "Firebase Admin service-account JSON path")
	projectID := flag.String("fcm-project-id", "", "Firebase project id; defaults to service account project_id")
	dailyLimit := flag.Int("daily-limit", 100000, "maximum FCM deliveries across all subscriptions per UTC day")
	subscriptionDailyLimit := flag.Int("subscription-daily-limit", 1000, "maximum FCM deliveries per subscription per UTC day")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("notify-server version %s\n", version.Version)
		return
	}

	store, err := relay.OpenStore(*statePath, *dailyLimit, *subscriptionDailyLimit)
	if err != nil {
		log.Fatalf("open relay state: %v", err)
	}
	sender, err := fcm.NewSender(fcm.SendOptions{
		ServiceAccountPath: *serviceAccount,
		ProjectID:          *projectID,
	})
	if err != nil {
		log.Fatalf("initialize FCM sender: %v", err)
	}
	relayServer, err := relay.NewServer(relay.ServerOptions{
		Store:     store,
		Sender:    sender,
		PublicURL: *publicURL,
		Logf:      log.Printf,
	})
	if err != nil {
		log.Fatalf("initialize relay: %v", err)
	}

	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           relayServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("relay shutdown: %v", err)
		}
	}()

	log.Printf("private-notify relay %s listening on %s (public %s, UTC limits %d total/%d per subscription)", version.Version, *listen, *publicURL, *dailyLimit, *subscriptionDailyLimit)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve relay: %v", err)
	}
}
