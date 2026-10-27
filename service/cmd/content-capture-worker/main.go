package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"service/contentcapture"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type settings struct {
	listenAddress string
	secret        string
	enabled       bool
	networkGate   bool
	cdpURL        string
	allowedHosts  []string
}

func main() {
	configuration, err := loadSettings(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", configuration.listenAddress)
	if err != nil {
		log.Fatal("capture worker loopback listener unavailable")
	}
	defer listener.Close()

	capturer := contentcapture.New(contentcapture.Config{
		Enabled:         configuration.enabled,
		NetworkIsolated: configuration.networkGate,
		CDPURL:          configuration.cdpURL,
		AllowedHosts:    configuration.allowedHosts,
	})
	handlerEnabled := configuration.enabled && configuration.networkGate && configuration.cdpURL != ""
	mux := http.NewServeMux()
	mux.Handle("/v1/capture", contentcapture.NewHandler(handlerEnabled, configuration.secret, capturer))
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      125 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		context, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(context)
	}()
	log.Print("content capture worker listening on loopback")
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("capture worker stopped unexpectedly")
	}
}

func loadSettings(getenv func(string) string) (settings, error) {
	configuration := settings{
		listenAddress: strings.TrimSpace(getenv("CONTENT_CAPTURE_LISTEN")),
		secret:        getenv("CONTENT_CAPTURE_SECRET"),
		enabled:       environmentBool(getenv("CONTENT_CAPTURE_ENABLED")),
		networkGate:   environmentBool(getenv("CONTENT_CAPTURE_NETWORK_ISOLATED")),
		cdpURL:        strings.TrimSpace(getenv("CONTENT_CAPTURE_CDP_URL")),
	}
	if configuration.listenAddress == "" {
		configuration.listenAddress = "127.0.0.1:19091"
	}
	if len(configuration.secret) < 32 {
		return settings{}, errors.New("CONTENT_CAPTURE_SECRET must contain at least 32 characters")
	}
	if !loopbackListenAddress(configuration.listenAddress) {
		return settings{}, errors.New("CONTENT_CAPTURE_LISTEN must be a numeric loopback address")
	}
	for _, host := range strings.Split(getenv("CONTENT_CAPTURE_ALLOWED_HOSTS"), ",") {
		if host = strings.TrimSpace(host); host != "" {
			configuration.allowedHosts = append(configuration.allowedHosts, host)
		}
	}
	return configuration, nil
}

func environmentBool(raw string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && value
}

func loopbackListenAddress(raw string) bool {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || port == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
