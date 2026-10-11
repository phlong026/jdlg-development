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
	Enabled         bool
	PaymentEnabled  bool
	ProtocolVersion string
	ProductID       string
	PriceFen        int64
	ManifestPath    string
	LeaseDuration   time.Duration
	JobTimeout      time.Duration
	DownloadTTL     time.Duration
	MaxAttempts     int
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
	price, err := strconv.ParseInt(valueOr("REPORT_PRICE_FEN", "0"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_PRICE_FEN")
	}
	maxAttempts, err := strconv.Atoi(valueOr("REPORT_MAX_ATTEMPTS", "3"))
	if err != nil {
		return Config{}, fmt.Errorf("report config: invalid REPORT_MAX_ATTEMPTS")
	}
	config := Config{
		Enabled:         enabled,
		PaymentEnabled:  paymentEnabled,
		ProtocolVersion: valueOr("REPORT_PROTOCOL_VERSION", "report-service-v1"),
		ProductID:       strings.TrimSpace(os.Getenv("REPORT_PRODUCT_ID")),
		PriceFen:        price,
		ManifestPath:    strings.TrimSpace(os.Getenv("REPORT_PACK_MANIFEST")),
		LeaseDuration:   durationOr("REPORT_LEASE_DURATION", 150*time.Second),
		JobTimeout:      durationOr("REPORT_JOB_TIMEOUT", 2*time.Minute),
		DownloadTTL:     durationOr("REPORT_DOWNLOAD_TTL", 10*time.Minute),
		MaxAttempts:     maxAttempts,
	}
	if config.ProtocolVersion == "" || config.PriceFen < 0 || config.MaxAttempts < 1 || config.MaxAttempts > 10 || config.LeaseDuration <= config.JobTimeout+10*time.Second || config.JobTimeout <= 0 || config.JobTimeout > 2*time.Minute || config.DownloadTTL <= 0 || config.DownloadTTL > time.Hour {
		return Config{}, fmt.Errorf("report config: invalid bounded setting")
	}
	if paymentEnabled && (!enabled || config.ProductID == "" || config.PriceFen <= 0 || !filepath.IsAbs(config.ManifestPath)) {
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
