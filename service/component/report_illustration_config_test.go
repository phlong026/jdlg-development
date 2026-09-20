package component

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"service/config"
	"service/model"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReportIllustrationConfigUsesEffectiveMiniProgramProvider(t *testing.T) {
	p := &AITaskProcessor{cfg: &config.Config{AI: config.AIConfig{LaoZhangAPIKey: "test-only", LaoZhangImageBaseURL: "https://images.example.com", LaoZhangImageModel: "gpt-image-configured"}}}
	selected, err := p.resolveReportIllustrationConfig(func() (*model.AIAPIConfig, error) {
		return &model.AIAPIConfig{ProviderCode: "gpt-image-2-vip", ProtocolType: "stale", APIEndpoint: "https://stale.example.com", Method: "POST"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := newReportIllustrationFromAPIConfig(selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider, usedModel, version := client.Identity()
	if provider != "laozhang-gpt-image" || usedModel != "gpt-image-configured" || len(version) != 64 || client.endpoint != "https://images.example.com/v1/images/generations" {
		t.Fatalf("identity/config mismatch %q %q %q", provider, usedModel, version)
	}
}

func TestReportIllustrationConfigRejectsIncompatibleActiveProvider(t *testing.T) {
	p := &AITaskProcessor{cfg: &config.Config{AI: config.AIConfig{LaoZhangAPIKey: "test-only", LaoZhangImageBaseURL: "https://images.example.com", LaoZhangImageModel: "gpt-image-configured"}}}
	for _, provider := range []string{"toapis", "runninghub", "laozhang", "unknown"} {
		t.Run(provider, func(t *testing.T) {
			_, err := p.resolveReportIllustrationConfig(func() (*model.AIAPIConfig, error) {
				return &model.AIAPIConfig{ProviderCode: provider, APIEndpoint: "https://other.example.com", Method: http.MethodPost}, nil
			})
			if err == nil {
				t.Fatal("silently used environment instead of incompatible selected provider")
			}
		})
	}
	if _, err := p.resolveReportIllustrationConfig(func() (*model.AIAPIConfig, error) { return nil, errors.New("private database error") }); err == nil || strings.Contains(err.Error(), "private database") {
		t.Fatal("configuration failure must be fail closed and sanitized", err)
	}
}

func TestReportIllustrationConfiguredFactoryRequiresDatabase(t *testing.T) {
	if _, err := NewConfiguredReportIllustrationClient(nil, nil, nil, nil); err == nil {
		t.Fatal("nil processor")
	}
}

type reportImageConfigDriver struct {
	provider string
	queryErr error
	missing  bool
	active   bool
}
type reportImageConfigConn struct{ config reportImageConfigDriver }
type reportImageConfigRows struct {
	provider string
	done     bool
	active   bool
}

var reportImageConfigDriverID atomic.Uint64

func (d reportImageConfigDriver) Open(string) (driver.Conn, error) {
	return &reportImageConfigConn{config: d}, nil
}
func (c *reportImageConfigConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *reportImageConfigConn) Close() error              { return nil }
func (c *reportImageConfigConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c *reportImageConfigConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 10*time.Second || !strings.Contains(q, "ORDER BY is_active DESC,id ASC LIMIT 1") || len(args) != 1 || args[0].Value != "ai_draw" {
		return nil, errors.New("missing config selection contract")
	}
	if c.config.queryErr != nil {
		return nil, c.config.queryErr
	}
	return &reportImageConfigRows{provider: c.config.provider, done: c.config.missing, active: c.config.active}, nil
}
func (r *reportImageConfigRows) Columns() []string {
	return []string{"provider_code", "provider_name", "protocol_type", "api_endpoint", "method", "api_key", "api_key_location", "api_key_name", "headers", "body_template", "is_active"}
}
func (r *reportImageConfigRows) Close() error { return nil }
func (r *reportImageConfigRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, []driver.Value{r.provider, "test channel", "openai_image_sync", "https://stale.example.com/v1/images/generations", "POST", "test-only-db-key", "header_bearer", "Authorization", "{}", "{}", r.active})
	return nil
}
func TestReportIllustrationConfiguredFactoryReadsSQLWithoutRedis(t *testing.T) {
	cfg := &config.Config{AI: config.AIConfig{LaoZhangAPIKey: "test-only-config-key", LaoZhangImageBaseURL: "https://images.example.com", LaoZhangImageModel: "configured-model"}}
	for _, tc := range []struct {
		name      string
		driver    reportImageConfigDriver
		wantError bool
	}{
		{"selected", reportImageConfigDriver{provider: "gpt-image-2-vip", active: true}, false},
		{"all-disabled", reportImageConfigDriver{provider: "gpt-image-2-vip", active: false}, true},
		{"empty", reportImageConfigDriver{missing: true}, false},
		{"incompatible", reportImageConfigDriver{provider: "toapis", active: true}, true},
		{"failed", reportImageConfigDriver{queryErr: errors.New("private backend details")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := fmt.Sprintf("report-image-config-%d", reportImageConfigDriverID.Add(1))
			sql.Register(name, tc.driver)
			db, err := sql.Open(name, "")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			client, err := NewConfiguredReportIllustrationClient(context.Background(), db, cfg, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("client=%v error=%v", client != nil, err)
			}
			if err != nil && strings.Contains(err.Error(), "private backend") {
				t.Fatal("database details exposed")
			}
			if err == nil && (client.model != "configured-model" || client.apiKey != "test-only-config-key") {
				t.Fatal("runtime mini-program override not reused")
			}
		})
	}
}
