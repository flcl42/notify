package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flcl42/notify/rep/internal/adb"
	"github.com/flcl42/notify/rep/internal/config"
	"github.com/flcl42/notify/rep/internal/fcm"
	"github.com/flcl42/notify/rep/internal/protocol"
	"github.com/flcl42/notify/rep/internal/qr"
	"github.com/flcl42/notify/rep/internal/registration"
	"github.com/flcl42/notify/rep/internal/version"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func getLanAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

func randomUUID() string {
	return uuid.Must(uuid.NewRandom()).String()
}

func randomKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return protocol.BytesToBase64URL(b)
}

func loadConfigPath() (string, error) {
	return config.GetConfigPath()
}

func promptForTitle(initial string) (string, error) {
	title := strings.TrimSpace(initial)
	if title != "" {
		return title, nil
	}
	fmt.Print("Notification title: ")
	reader := bufio.NewReader(os.Stdin)
	title, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(title), nil
}

func waitForKeypress() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		select {}
	}
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	buf := make([]byte, 1)
	_, err = os.Stdin.Read(buf)
	return err
}

func createSubscription(titleInput string, port int, host string, waitSeconds int, adbPath string, noUSB bool, replace bool) error {
	title, err := promptForTitle(titleInput)
	if err != nil {
		return err
	}
	if title == "" {
		return fmt.Errorf("notification title is required")
	}

	cfgPath, err := loadConfigPath()
	if err != nil {
		return err
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	existing := config.FindSubscriptionByTitle(cfg, title)
	if existing != nil && !replace {
		return fmt.Errorf("title already exists: %s. Use --replace to rotate and re-pair it", title)
	}

	sub := config.Subscription{
		ID:           randomUUID(),
		Title:        title,
		Name:         title,
		DefaultTitle: title,
		Key:          randomKey(),
		Delivery:     "push",
		PushTokens:   []config.PushToken{},
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}

	if _, err := config.UpsertSubscription(cfgPath, sub, replace); err != nil {
		return err
	}

	if port == 0 {
		port = cfg.DefaultPairPort
	}
	if port == 0 {
		port = 8788
	}

	bindHost := host
	publicHost := host
	if host == "0.0.0.0" || host == "::" {
		publicHost = getLanAddress()
	}

	if !noUSB {
		adbExe := adb.FindAdb(adbPath)
		if adbExe != "" && adb.HasConnectedDevice(adbExe) {
			if err := adb.SetupReverse(adbExe, port); err == nil {
				bindHost = "127.0.0.1"
				publicHost = "127.0.0.1"
				fmt.Printf("ADB reverse active: tcp:%d -> tcp:%d\n", port, port)
			} else {
				fmt.Println("ADB reverse not available; using LAN registration URL.")
			}
		} else {
			fmt.Println("ADB reverse not available; using LAN registration URL.")
		}
	}

	registrationURL := fmt.Sprintf("http://%s:%d/register", publicHost, port)

	server := registration.New(sub.ID, port, bindHost, func(reg registration.Registration) error {
		_, _, err := config.AddPushRegistration(cfgPath, sub.ID, config.PushToken{
			Provider:     reg.Provider,
			Token:        reg.Token,
			Platform:     reg.Platform,
			RegisteredAt: reg.RegisteredAt,
		})
		return err
	})

	if err := server.Start(); err != nil {
		return err
	}
	defer server.Close()

	pairingURL, err := protocol.CreatePairingURL(protocol.Subscription{
		ID:           sub.ID,
		Title:        sub.Title,
		Name:         sub.Name,
		DefaultTitle: sub.DefaultTitle,
		Key:          sub.Key,
		CreatedAt:    sub.CreatedAt,
	}, registrationURL)
	if err != nil {
		return err
	}

	fmt.Printf("Title: %s\n", sub.Title)
	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Printf("Registration: %s\n", registrationURL)
	fmt.Println("Scan this QR in the Android app. Treat it like a private key.")
	if err := qr.PrintTerminal(pairingURL); err != nil {
		return err
	}
	fmt.Println(pairingURL)
	fmt.Println("Waiting for phone push-token registration. Press any key to stop...")

	timeout := time.After(time.Duration(waitSeconds) * time.Second)
	registeredCh := server.Registered()
	keypressCh := make(chan error, 1)
	go func() {
		keypressCh <- waitForKeypress()
	}()

	select {
	case reg := <-registeredCh:
		fmt.Printf("Registered 1 push token(s) for \"%s\".\n", reg.Token)
	case <-timeout:
		return fmt.Errorf("timed out waiting for mobile push registration")
	case <-keypressCh:
		fmt.Println("Stopped. The private key remains in rep.yaml; run create --replace to rotate it.")
	}

	return nil
}

func resolveTitleAndBody(cfg config.Config, args []string) (*config.Subscription, string) {
	for length := len(args) - 1; length >= 1; length-- {
		candidate := strings.Join(args[:length], " ")
		if sub := config.FindSubscriptionByTitle(cfg, candidate); sub != nil {
			return sub, strings.Join(args[length:], " ")
		}
	}
	return nil, ""
}

func sendNotification(args []string, service string, fcmServiceAccount string, fcmProjectID string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: rep <title> <notification text>")
	}

	cfgPath, err := loadConfigPath()
	if err != nil {
		return err
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	sub, body := resolveTitleAndBody(cfg, args)
	if sub == nil {
		titles := []string{}
		for _, s := range cfg.Subscriptions {
			titles = append(titles, s.Title)
		}
		return fmt.Errorf("no title matched. Known titles: %s", strings.Join(titles, ", "))
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("notification text is required after the title")
	}

	envelope, err := protocol.EncryptNotification(protocol.Subscription{
		ID:           sub.ID,
		Title:        sub.Title,
		Name:         sub.Name,
		DefaultTitle: sub.DefaultTitle,
		Key:          sub.Key,
		CreatedAt:    sub.CreatedAt,
	}, protocol.Notification{
		V:              "1",
		ID:             randomUUID(),
		SubscriptionID: sub.ID,
		Service:        service,
		Title:          sub.Title,
		Body:           body,
		Data:           map[string]interface{}{},
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}

	var pushTokens []fcm.PushToken
	for _, t := range sub.PushTokens {
		pushTokens = append(pushTokens, fcm.PushToken{Provider: t.Provider, Token: t.Token})
	}

	result, err := fcm.SendPushNotifications(pushTokens, fcm.Envelope{
		SubscriptionID: envelope.SubscriptionID,
	}, fcm.SendOptions{
		Service:            service,
		ServiceAccountPath: config.ResolveFcmServiceAccount(cfg, fcmServiceAccount),
		ProjectID:          fcmProjectID,
	})
	if err != nil {
		return err
	}

	if result.Sent == 0 {
		return fmt.Errorf("no FCM push tokens are registered for \"%s\". Run: rep create \"%s\"", sub.Title, sub.Title)
	}

	fmt.Printf("Sent \"%s\" notification. Tokens: %d.\n", sub.Title, result.Sent)
	return nil
}

func listSubscriptions() error {
	cfgPath, err := loadConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	fmt.Printf("Config: %s\n", cfgPath)
	if len(cfg.Subscriptions) == 0 {
		fmt.Println("No titles yet.")
		return nil
	}

	for _, sub := range cfg.Subscriptions {
		fmt.Printf("%s\tpush tokens %d\tcreated %s\n", sub.Title, len(sub.PushTokens), sub.CreatedAt)
	}
	return nil
}

func printConfigPath() error {
	cfgPath, err := loadConfigPath()
	if err != nil {
		return err
	}
	fmt.Println(cfgPath)
	return nil
}

func saveCredential(path string) error {
	if path == "" {
		return fmt.Errorf("service-account JSON path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	cfgPath, err := loadConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	cfg.FcmServiceAccount = abs
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Stored FCM credential path in %s\n", cfgPath)
	return nil
}

func main() {
	var (
		fcmServiceAccount string
		fcmProjectID      string
		service           string
	)

	rootCmd := &cobra.Command{
		Use:   "rep [args...]",
		Short: "Send encrypted Android notifications by title.",
		Long:  "rep sends encrypted Android notifications by title. Provide the title followed by the notification text.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNotification(args, service, fcmServiceAccount, fcmProjectID)
		},
	}
	rootCmd.Version = version.Version
	rootCmd.PersistentFlags().StringVar(&fcmServiceAccount, "fcm-service-account", "", "Firebase Admin service-account JSON path for this send")
	rootCmd.PersistentFlags().StringVar(&fcmProjectID, "fcm-project-id", "", "Firebase project id; defaults to the service account project_id")
	rootCmd.PersistentFlags().StringVar(&service, "service", "rep", "source service name")

	var (
		createPort       int
		createHost       string
		createWait       int
		createAdb        string
		createNoUSB      bool
		createReplace    bool
	)
	createCmd := &cobra.Command{
		Use:   "create [title]",
		Short: "Create a private notification key for a title, print a QR, and register the phone.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := ""
			if len(args) > 0 {
				title = args[0]
			}
			return createSubscription(title, createPort, createHost, createWait, createAdb, createNoUSB, createReplace)
		},
	}
	createCmd.Flags().IntVarP(&createPort, "port", "p", 0, "pairing registration port")
	createCmd.Flags().StringVar(&createHost, "host", "0.0.0.0", "registration bind host when USB reverse is unavailable")
	createCmd.Flags().IntVar(&createWait, "wait-seconds", 600, "seconds to wait for phone registration")
	createCmd.Flags().StringVar(&createAdb, "adb", "", "adb executable path")
	createCmd.Flags().BoolVar(&createNoUSB, "no-usb", false, "do not try adb reverse; use LAN registration instead")
	createCmd.Flags().BoolVar(&createReplace, "replace", false, "replace an existing title with a new private key")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured notification titles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listSubscriptions()
		},
	}

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Print rep.yaml path.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printConfigPath()
		},
	}

	credentialCmd := &cobra.Command{
		Use:   "credential <path>",
		Short: "Store the Firebase Admin service-account JSON path in rep.yaml.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return saveCredential(args[0])
		},
	}

	rootCmd.AddCommand(createCmd, listCmd, configCmd, credentialCmd)

	// Suppress default usage/error printing.
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		os.Exit(1)
	}
}
