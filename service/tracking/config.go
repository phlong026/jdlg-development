package tracking

import (
	_ "embed"
	"encoding/json"
	"os"
)

//go:embed routes.json
var routeJSON []byte

func LoadConfig() Config {
	c := Config{Enabled: os.Getenv("TRACKING_ENABLED") == "true", ContentEnabled: os.Getenv("TRACKING_CONTENT_ENABLED") == "true", VideoEnabled: os.Getenv("TRACKING_VIDEO_ENABLED") == "true", IntentEnabled: os.Getenv("TRACKING_INTENT_ENABLED") == "true", Tenant: os.Getenv("TRACKING_TENANT"), DefaultOwner: os.Getenv("TRACKING_DEFAULT_OWNER"), ConsentVersion: os.Getenv("TRACKING_CONSENT_VERSION"), PrivacyNotice: os.Getenv("TRACKING_PRIVACY_NOTICE"), RawDays: 90, RetentionDays: 180, Routes: map[string]Route{}}
	_ = json.Unmarshal(routeJSON, &c.Routes)
	if c.Tenant == "" || c.DefaultOwner == "" || c.ConsentVersion == "" || c.PrivacyNotice == "" {
		c.Enabled = false
	}
	return c
}

// routeAudience resolves a page's consumption domain, defaulting to neutral so
// unlabelled or future routes can never skew customer-domain gating.
func (s *Service) routeAudience(route string) string {
	if r, ok := s.Config.Routes[route]; ok {
		switch r.Audience {
		case RouteAudienceCustomer, RouteAudienceBSide:
			return r.Audience
		}
	}
	return RouteAudienceNeutral
}
