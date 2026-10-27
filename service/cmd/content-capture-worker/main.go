package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"service/contentcapture"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	captureSocketDirectory = "/run/jdlg-content-capture"
	captureSocketPath      = captureSocketDirectory + "/capture.sock"
)

type settings struct {
	listenAddress   string
	secret          string
	enabled         bool
	networkGate     bool
	cdpURL          string
	cdpDiscoveryURL string
	allowedHosts    []string
}

func main() {
	configuration, err := loadSettings(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := listenCapture(configuration.listenAddress)
	if err != nil {
		log.Fatal("capture worker local listener unavailable")
	}
	defer listener.Close()

	capturer := contentcapture.New(contentcapture.Config{
		Enabled:         configuration.enabled,
		NetworkIsolated: configuration.networkGate,
		CDPURL:          configuration.cdpURL,
		CDPDiscoveryURL: configuration.cdpDiscoveryURL,
		AllowedHosts:    configuration.allowedHosts,
	})
	handlerEnabled := configuration.enabled && configuration.networkGate && (configuration.cdpURL != "" || configuration.cdpDiscoveryURL != "")
	mux := http.NewServeMux()
	handler := contentcapture.NewHandler(handlerEnabled, configuration.secret, capturer)
	mux.Handle("/v1/capture", handler)
	mux.Handle("/health/ready", handler)
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
	log.Print("content capture worker listening locally")
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("capture worker stopped unexpectedly")
	}
}

func loadSettings(getenv func(string) string) (settings, error) {
	configuration := settings{
		listenAddress:   strings.TrimSpace(getenv("CONTENT_CAPTURE_LISTEN")),
		secret:          getenv("CONTENT_CAPTURE_SECRET"),
		enabled:         environmentBool(getenv("CONTENT_CAPTURE_ENABLED")),
		networkGate:     environmentBool(getenv("CONTENT_CAPTURE_NETWORK_ISOLATED")),
		cdpURL:          strings.TrimSpace(getenv("CONTENT_CAPTURE_CDP_URL")),
		cdpDiscoveryURL: strings.TrimSpace(getenv("CONTENT_CAPTURE_CDP_DISCOVERY_URL")),
	}
	if configuration.listenAddress == "" {
		configuration.listenAddress = "127.0.0.1:19091"
	}
	if len(configuration.secret) < 32 {
		return settings{}, errors.New("CONTENT_CAPTURE_SECRET must contain at least 32 characters")
	}
	if !validListenAddress(configuration.listenAddress) {
		return settings{}, errors.New("CONTENT_CAPTURE_LISTEN must be numeric loopback or the dedicated unix socket")
	}
	for _, host := range strings.Split(getenv("CONTENT_CAPTURE_ALLOWED_HOSTS"), ",") {
		if host = strings.TrimSpace(host); host != "" {
			configuration.allowedHosts = append(configuration.allowedHosts, host)
		}
	}
	return configuration, nil
}

func validListenAddress(raw string) bool {
	if strings.HasPrefix(raw, "unix:") {
		return raw == "unix:"+captureSocketPath
	}
	return loopbackListenAddress(raw)
}

func listenCapture(raw string) (net.Listener, error) {
	if strings.HasPrefix(raw, "unix:") {
		return listenUnixCapture(strings.TrimPrefix(raw, "unix:"), captureSocketDirectory)
	}
	return net.Listen("tcp", raw)
}

func listenUnixCapture(path, requiredDirectory string) (net.Listener, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) != requiredDirectory || filepath.Base(path) != "capture.sock" {
		return nil, errors.New("capture socket path is outside the dedicated runtime directory")
	}
	directory, err := os.Lstat(requiredDirectory)
	if err != nil || !directory.IsDir() || directory.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("capture socket runtime directory is unavailable or unsafe")
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, errors.New("capture socket already exists; remove it before startup only after the prior service stops")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("capture socket path cannot be inspected")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0660); err != nil {
		_ = listener.Close()
		return nil, errors.New("capture socket permissions cannot be restricted")
	}
	return listener, nil
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
