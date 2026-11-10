package tracking

import (
	_ "embed"
	"encoding/json"
	"os"
)

//go:embed routes.json
var routeJSON []byte

// Tracking consent decision (2026-11-10, P7): TRACKING_CONSENT_VERSION must NOT be
// bumped when only TRACKING_PRIVACY_NOTICE changes. Before the T7.3 client migration
// rule, bumping the version reset every stored choice, so a default-on migration could
// silently re-enable users who had withdrawn. The client now keeps an explicit decline
// authoritative across both notice and version changes; the product decision is still
// "no bump". Ops may edit the notice text without touching the version, but never bump
// the version without a reviewed declaration change.
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
