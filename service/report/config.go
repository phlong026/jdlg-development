package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Enabled              bool
	PaymentEnabled       bool
	LegacyPaymentBlocked bool
	MultiAgentEnabled    bool
	DecisionEnabled      bool
	HouseEnabled         bool
	EmbeddedWorker       bool
	WorkerConcurrency    int
	ProtocolVersion      string
	ProductID            string
	PriceFen             int64
	ManifestPath         string
	LeaseDuration        time.Duration
	JobTimeout           time.Duration
	DownloadTTL          time.Duration
	MaxAttempts          int
}

func LoadConfig() (Config, error) {
	enabled, err := strconv.ParseBool(valueOr("REPORT_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_ENABLED")
	}
	paymentEnabled, err := strconv.ParseBool(valueOr("REPORT_PAYMENT_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_PAYMENT_ENABLED")
	}
	multiAgent, err := strconv.ParseBool(valueOr("REPORT_MULTI_AGENT_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_MULTI_AGENT_ENABLED")
	}
	decisionEnabled, err := strconv.ParseBool(valueOr("REPORT_DECISION_ENABLED", "false"))
	if err != nil || decisionEnabled && !multiAgent {
		return Config{}, fmt.Errorf("report config: REPORT_DECISION_ENABLED requires multi-agent execution")
	}
	houseEnabled, err := strconv.ParseBool(valueOr("REPORT_HOUSE_ENABLED", "false"))
	if err != nil || houseEnabled && !decisionEnabled {
		return Config{}, fmt.Errorf("report config: REPORT_HOUSE_ENABLED requires decision execution")
	}
	embeddedWorker, err := strconv.ParseBool(valueOr("REPORT_WORKER_EMBEDDED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_WORKER_EMBEDDED")
	}
	concurrency, err := strconv.Atoi(valueOr("REPORT_WORKER_CONCURRENCY", "1"))
	if err != nil || concurrency < 1 || concurrency > 8 {
		return Config{}, fmt.Errorf("report config: REPORT_WORKER_CONCURRENCY must be 1..8")
	}
	protocol, timeout, lease := "report-service-v1", 2*time.Minute, 150*time.Second
	if multiAgent {
		protocol, timeout, lease = "report-agents-v1", 5*time.Minute, 330*time.Second
	}
	price, err := strconv.ParseInt(valueOr("REPORT_PRICE_FEN", "0"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_PRICE_FEN")
	}
	maxAttempts, err := strconv.Atoi(valueOr("REPORT_MAX_ATTEMPTS", "3"))
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_MAX_ATTEMPTS")
	}
	config := Config{
		Enabled:           enabled,
		PaymentEnabled:    paymentEnabled,
		MultiAgentEnabled: multiAgent,
		DecisionEnabled:   decisionEnabled,
		HouseEnabled:      houseEnabled,
		EmbeddedWorker:    embeddedWorker,
		WorkerConcurrency: concurrency,
		ProtocolVersion:   valueOr("REPORT_PROTOCOL_VERSION", protocol),
		ProductID:         strings.TrimSpace(os.Getenv("REPORT_PRODUCT_ID")),
		PriceFen:          price,
		ManifestPath:      strings.TrimSpace(os.Getenv("REPORT_PACK_MANIFEST")),
		LeaseDuration:     durationOr("REPORT_LEASE_DURATION", lease),
		JobTimeout:        durationOr("REPORT_JOB_TIMEOUT", timeout),
		DownloadTTL:       durationOr("REPORT_DOWNLOAD_TTL", 10*time.Minute),
		MaxAttempts:       maxAttempts,
	}
	if config.ProtocolVersion == "" || config.PriceFen < 0 || config.MaxAttempts < 1 || config.MaxAttempts > 10 || config.LeaseDuration <= config.JobTimeout+10*time.Second || config.JobTimeout <= 0 || config.JobTimeout > 10*time.Minute || config.DownloadTTL <= 0 || config.DownloadTTL > time.Hour {
		return Config{}, fmt.Errorf("report config: invalid bounded setting")
	}
	if houseEnabled && config.JobTimeout < 4*time.Minute {
		return Config{}, fmt.Errorf("report config: REPORT_HOUSE_ENABLED requires job timeout of at least 4m for 2K image requests")
	}
	if paymentEnabled && (!enabled || config.ProductID == "" || config.PriceFen <= 0 || !decisionEnabled && !filepath.IsAbs(config.ManifestPath)) {
		return Config{}, fmt.Errorf("report config: payment requires enabled module, product, price and absolute manifest path")
	}
	return config, nil
}

func valueOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationOr(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return -1
	}
	return duration
}
