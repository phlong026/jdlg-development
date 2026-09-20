package component

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"service/config"
	"service/model"
)

var errReportImageChannelDisabled = errors.New("selected mini-program image channel is disabled")

// NewConfiguredReportIllustrationClient uses the mini-program's selected
// ai_draw channel and runtime GPT-Image defaults, without creating a customer
// task or charging their stones. Unsupported active channels fail closed.
func NewConfiguredReportIllustrationClient(ctx context.Context, db *sql.DB, cfg *config.Config, httpClient *http.Client) (*ReportIllustrationClient, error) {
	if ctx == nil || db == nil || cfg == nil {
		return nil, errors.New("report illustration configuration store is unavailable")
	}
	processor := &AITaskProcessor{cfg: cfg}
	lookup := func() (*model.AIAPIConfig, error) {
		queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var code, name, protocol, endpoint, method, key, location, keyName, headers, body sql.NullString
		var active sql.NullBool
		err := db.QueryRowContext(queryCtx, `SELECT provider_code,provider_name,protocol_type,api_endpoint,method,api_key,api_key_location,api_key_name,headers,body_template,is_active FROM ai_api_config WHERE task_type=? ORDER BY is_active DESC,id ASC LIMIT 1`, "ai_draw").Scan(&code, &name, &protocol, &endpoint, &method, &key, &location, &keyName, &headers, &body, &active)
		if err != nil {
			return nil, err
		}
		// A stopped channel must stay stopped for paid report generation. Only
		// absence of any configured row may use the environment-backed default.
		if !active.Valid || !active.Bool {
			return nil, errReportImageChannelDisabled
		}
		return &model.AIAPIConfig{ProviderCode: code.String, ProviderName: name.String, ProtocolType: protocol.String, APIEndpoint: endpoint.String, Method: method.String, APIKey: key.String, APIKeyLocation: location.String, APIKeyName: keyName.String, Headers: headers.String, BodyTemplate: body.String}, nil
	}
	selected, err := processor.resolveReportIllustrationConfig(lookup)
	if err != nil {
		return nil, err
	}
	return newReportIllustrationFromAPIConfig(selected, httpClient)
}

func (p *AITaskProcessor) resolveReportIllustrationConfig(lookup func() (*model.AIAPIConfig, error)) (*AIAPIConfigData, error) {
	if p == nil || p.cfg == nil {
		return nil, errors.New("report illustration configuration is unavailable")
	}
	var selected *AIAPIConfigData
	if lookup != nil {
		row, err := lookup()
		if errors.Is(err, errReportImageChannelDisabled) {
			return nil, err
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[report-image] error: selected ai_draw configuration cannot be read")
			return nil, errors.New("report illustration configuration lookup failed")
		}
		if err == nil && row != nil {
			// Match getConfiguredAIConfig's normalization and runtime override, but
			// do not silently replace an unsupported active channel with another one.
			code := strings.TrimSpace(row.ProviderCode)
			if code == "" {
				code = "laozhang"
			}
			selected = &AIAPIConfigData{TaskType: "ai_draw", ProviderCode: code, ProviderName: strings.TrimSpace(row.ProviderName), ProtocolType: strings.TrimSpace(row.ProtocolType), APIEndpoint: strings.TrimSpace(row.APIEndpoint), Method: strings.TrimSpace(row.Method), APIKey: strings.TrimSpace(row.APIKey), APIKeyLocation: strings.TrimSpace(row.APIKeyLocation), APIKeyName: strings.TrimSpace(row.APIKeyName), Headers: row.Headers, BodyTemplate: row.BodyTemplate}
			if selected.Method == "" {
				selected.Method = http.MethodPost
			}
			selected = p.applyRuntimeAIDrawProviderDefaults(selected, "ai_draw", false)
			selected = p.normalizeRuntimeAIConfigData(selected, "ai_draw", false)
		}
	}
	if selected == nil {
		selected = p.getHardcodedAIConfig("ai_draw", false)
	}
	if selected == nil || selected.ProviderCode != "gpt-image-2-vip" || selected.ProtocolType != "openai_image_sync" {
		return nil, errors.New("selected mini-program image channel is incompatible with report illustrations")
	}
	return selected, nil
}

func newReportIllustrationFromAPIConfig(selected *AIAPIConfigData, httpClient *http.Client) (*ReportIllustrationClient, error) {
	if selected == nil || selected.ProviderCode != "gpt-image-2-vip" || selected.ProtocolType != "openai_image_sync" || selected.Method != http.MethodPost || selected.APIKeyLocation != "header_bearer" || selected.APIKeyName != "Authorization" {
		return nil, errors.New("selected image protocol is unsupported")
	}
	var body struct {
		Model string `json:"model"`
	}
	if json.Unmarshal([]byte(selected.BodyTemplate), &body) != nil || strings.TrimSpace(body.Model) == "" {
		return nil, errors.New("selected image model is unavailable")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(selected.APIEndpoint), "/")
	const suffix = "/v1/images/generations"
	if !strings.HasSuffix(endpoint, suffix) {
		return nil, errors.New("selected image endpoint is unsupported")
	}
	return NewReportIllustrationClient(&config.Config{AI: config.AIConfig{LaoZhangAPIKey: selected.APIKey, LaoZhangImageBaseURL: strings.TrimSuffix(endpoint, suffix), LaoZhangImageModel: body.Model}}, httpClient)
}

// Identity excludes credentials. Rotating a key does not invalidate completed
// assets; changing endpoint, model or request contract does.
func (c *ReportIllustrationClient) Identity() (provider, model, configVersion string) {
	if c == nil {
		return "", "", ""
	}
	sum := sha256.Sum256([]byte(c.endpoint + "\x00" + c.model + "\x00report-illustration-2048x1152-v1"))
	return ReportIllustrationProvider, c.model, hex.EncodeToString(sum[:])
}
