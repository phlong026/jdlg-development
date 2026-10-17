package route

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"service/model"
	"service/tracking"
)

const (
	trackingLeadAutoSource   = "share_tracking_auto"
	trackingUpgradeEventType = "intent_upgrade"
	trackingLeadAutoPage     = 100
)

// feedbackIntentUpgrades appends an intent_upgrade event on every open lead
// whose owner-visitor now reads high after a recompute. Runs once per
// recompute, deduped per lead per day so repeated recalculations never spam
// the workbench. Failures are logged and skipped: analytics must never block
// or fail the recompute that triggered them.
func feedbackIntentUpgrades(ctx context.Context, svc *tracking.Service, leads *model.CustomerServiceModel, scope tracking.Scope, now time.Time) {
	if svc == nil || leads == nil || leads.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	list, err := svc.Visitors(ctx, scope, tracking.Filter{Intent: "high", Identity: "identified", Limit: trackingLeadAutoPage})
	if err != nil {
		log.Printf("[Tracking] intent upgrade feedback aborted: %v", err)
		return
	}
	visitors, _ := list.Items.([]tracking.Visitor)
	for _, visitor := range visitors {
		userID, ok := trackingVisitorAccount(visitor.Key)
		if !ok {
			continue
		}
		leadID, err := leads.FindLatestOpenLeadID(userID)
		if err != nil || leadID <= 0 {
			continue
		}
		lead, err := leads.GetLead(leadID)
		if err != nil || lead == nil {
			continue
		}
		events, err := leads.ListEventsForLead(lead, 5)
		if err != nil {
			continue
		}
		today := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
		already := false
		for _, event := range events {
			if event.EventType == trackingUpgradeEventType && event.Source == trackingLeadSource &&
				event.CreatedAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02") == today {
				already = true
				break
			}
		}
		if already {
			continue
		}
		reason := ""
		if len(visitor.Intent.Reasons) > 0 {
			reason = strings.TrimSpace(visitor.Intent.Reasons[0])
		}
		if _, err := leads.CreateEvent(&model.CustomerServiceEventCreateInput{
			UserID:      userID,
			LeadID:      leadID,
			EventType:   trackingUpgradeEventType,
			Source:      trackingLeadSource,
			IntentLevel: "high",
			DemandSummary: "客户分享浏览意向已升至高（近30天会话 " + strconv.Itoa(visitor.Sessions) +
				" 次、浏览 " + strconv.Itoa(visitor.Views) + " 页）" + reason,
			Payload: map[string]interface{}{
				"visitor_key": visitor.Key, "metrics": map[string]interface{}{
					"sessions": visitor.Sessions, "page_views": visitor.Views, "visible_ms": visitor.Visible,
				}, "customer_share": visitor.Intent.CustomerShare, "triggered_by": "intent_recompute",
			},
		}); err != nil {
			log.Printf("[Tracking] intent upgrade event failed lead=%d user=%d: %v", leadID, userID, err)
		}
	}
}

// startTrackingLeadAutomation launches the opt-in auto converter. Disabled
// unless TRACKING_LEAD_AUTO_ENABLED=true with a configured actor account that
// holds tracking CRM rights. The CRM handover rule stops it cold, matching the
// manual endpoint.
func startTrackingLeadAutomation(svc *tracking.Service, leads *model.CustomerServiceModel, identities *model.UserIdentityModel, users *model.UserModel) {
	if os.Getenv("TRACKING_LEAD_AUTO_ENABLED") != "true" {
		return
	}
	if leads == nil || leads.DB == nil || users == nil || users.DB == nil || crmEnabled() {
		log.Printf("[Tracking] lead auto conversion disabled: lead service or account store unavailable, or CRM owns customers")
		return
	}
	actor, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("TRACKING_LEAD_AUTO_ACTOR")), 10, 64)
	if err != nil || actor <= 0 {
		log.Printf("[Tracking] lead auto conversion disabled: TRACKING_LEAD_AUTO_ACTOR must be a management account id with tracking can_crm")
		return
	}
	interval := time.Hour
	if minutes, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TRACKING_LEAD_AUTO_INTERVAL_MIN"))); err == nil && minutes >= 15 {
		interval = time.Duration(minutes) * time.Minute
	}
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			scope, err := loadTrackingScopeFor(ctx, users.DB, tracking.LoadConfig().Tenant, actor)
			if err != nil || !scope.CRM {
				log.Printf("[Tracking] lead auto conversion skipped: scope for actor %d unavailable (%v)", actor, err)
			} else {
				autoConvertHighIntentVisitors(ctx, svc, leads, identities, scope)
			}
			cancel()
			time.Sleep(interval)
		}
	}()
	log.Printf("[Tracking] lead auto conversion enabled (actor=%d, interval=%s, source=%s)", actor, interval, trackingLeadAutoSource)
}

// autoConvertHighIntentVisitors sweeps identified high-intent visitors and
// converts the ones that pass the gate with no open lead. Individual failures
// are logged and skipped so one bad visitor never stops the sweep.
func autoConvertHighIntentVisitors(ctx context.Context, svc *tracking.Service, leads *model.CustomerServiceModel, identities *model.UserIdentityModel, scope tracking.Scope) {
	list, err := svc.Visitors(ctx, scope, tracking.Filter{Intent: "high", Identity: "identified", Limit: trackingLeadAutoPage})
	if err != nil {
		log.Printf("[Tracking] lead auto sweep aborted: %v", err)
		return
	}
	visitors, _ := list.Items.([]tracking.Visitor)
	converted := 0
	for _, visitor := range visitors {
		outcome, err := convertVisitorToLead(ctx, svc, leads, identities, scope, visitor.Key, trackingLeadAutoSource)
		if err != nil {
			var te *tracking.Error
			// 422/404 are expected gate refusals (anonymous, B-side, missing
			// visitors); anything else deserves a log line.
			if !errors.As(err, &te) || (te.Status != 422 && te.Status != 404) {
				log.Printf("[Tracking] lead auto convert failed visitor=%s: %v", visitor.Key, err)
			}
			continue
		}
		if !outcome.Existed {
			converted++
		}
	}
	if converted > 0 {
		log.Printf("[Tracking] lead auto sweep created %d lead(s)", converted)
	}
}
