package reportnotify

import (
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Endpoint string
	Token    string
}

func LoadConfig() (Config, error) {
	config := Config{Endpoint: strings.TrimSpace(os.Getenv("REPORT_NOTIFY_ENDPOINT")),
		Token: strings.TrimSpace(os.Getenv("REPORT_NOTIFY_TOKEN"))}
	return config, config.Validate()
}

func (c Config) Configured() bool { return c.Endpoint != "" && c.Token != "" }

func (c Config) Validate() error {
	if c.Endpoint == "" && c.Token == "" {
		return nil
	}
	parsed, err := url.Parse(c.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || !validText(c.Token, 4096) {
		return ErrInvalidConfig
	}
	return nil
}
