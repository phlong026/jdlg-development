package route

import (
	"context"
	"errors"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/notification"
	"service/officialaccount"
	"service/tracking"
)

// Only the verified account's field mapping enables external delivery. No
// guessed template keys or historical backlog are sent when a binding appears.

func registerPhase3Notifications(parent context.Context, public, mini *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) func() {
	ctx, cancel := context.WithCancel(parent)
	var jobs sync.WaitGroup
	start := func(run func(context.Context) error) {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			if err := run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[notification] error worker_stopped error_type=%T", err)
			}
		}()
	}
	officialCfg := officialaccount.LoadConfigFromEnv()
	fields, fieldsErr := parseNoticeTemplateFields(os.Getenv("OFFICIAL_ACCOUNT_BROWSE_FIELDS"))
	if os.Getenv("OFFICIAL_ACCOUNT_MINIPROGRAM_VERIFIED") == "true" {
		officialCfg.MiniProgramAppID = miniprogramCredentials().AppID
		officialCfg.MiniProgramPagePath = "pages/messagecenter/messagecenter"
	}
	if os.Getenv("OFFICIAL_ACCOUNT_DELIVERY_ENABLED") != "true" || fieldsErr != nil {
		officialCfg.TemplateID = ""
	}
	// A Mini Program credential cannot stand in for an independent service account.
	if officialCfg.AppID != "" && officialCfg.AppID == miniprogramCredentials().AppID {
		officialCfg.Enabled = false
		log.Printf("[notification] warn official_account_disabled reason=mini_app_id_reused")
	}
	var bindings *officialaccount.SQLStore
	if officialCfg.Enabled {
		var err error
		bindings, err = officialaccount.NewSQLStore(users.DB)
		if err == nil {
			verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = bindings.VerifySchema(verifyCtx)
			stop()
		}
		if err != nil {
			bindings = nil
			officialCfg.Enabled = false
			log.Printf("[notification] error official_account_disabled reason=schema_unavailable error_type=%T", err)
		}
	}
	// Cleanup participates even when inbox generation is turned off. It runs
	// only for persisted notice rows, so old deployments need no new tables.
	projections, projectionErr := notification.NewSQLStore(users.DB, svc.Config.Tenant)
	if projectionErr == nil {
		svc.OnRemoveNoticeEvents = func(ctx context.Context, ids []string) error {
			tx, ok := tracking.NoticeWriteTx(ctx)
			if !ok {
				return notification.ErrUnavailable
			}
			return projections.DeleteEventProjectionsTx(ctx, tx, ids)
		}
		svc.OnContentRevoke = func(ctx context.Context, contentID string) error {
			tx, ok := tracking.NoticeWriteTx(ctx)
			if !ok {
				return notification.ErrUnavailable
			}
			return projections.CancelPendingTx(ctx, tx, contentID, "", svc.Now())
		}
	}
	var inbox *notification.SQLStore
	if os.Getenv("NOTIFICATIONS_ENABLED") == "true" {
		var err error
		inbox, err = notification.NewSQLStore(users.DB, svc.Config.Tenant)
		if err == nil {
			verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = inbox.VerifySchema(verifyCtx)
			stop()
		}
		if err != nil {
			inbox = nil
			log.Printf("[notification] error inbox_disabled reason=schema_unavailable error_type=%T", err)
		}
	}
	var inboxAPI notification.InboxStore
	if inbox != nil {
		inboxAPI = inbox
	}
	RegisterNotificationRoutes(mini, inboxAPI, users, codes)
	deps := OfficialAccountDeps{Config: officialCfg}
	if bindings != nil {
		deps.Store = bindings
		deps.Provider = officialaccount.NewHTTPProvider(officialCfg)
	}
	// Delayed receipts remain durable while new inbox generation is disabled.
	// Missing/unavailable schema returns an explicit 503 from the callback.
	if projectionErr == nil {
		deps.Receipts = projections
	}
	RegisterOfficialAccountRoutes(public, mini, deps, users, codes)
	if inbox == nil {
		return func() { cancel(); jobs.Wait() }
	}
	eligible := func(ctx context.Context, event notification.Event) (bool, error) {
		var (
			scope tracking.Scope
			err   error
		)
		if event.Kind == "project_note_open" {
			scope, err = loadMiniTrackingMaterialScope(ctx, users.DB, svc.Config.Tenant, event.RecipientID)
		} else {
			scope, err = loadMiniTrackingMyScope(ctx, users.DB, svc.Config.Tenant, event.RecipientID)
		}
		if err != nil {
			var denied *tracking.Error
			if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403) {
				return false, nil
			}
			return false, err
		}
		return phase3NoticeEligible(ctx, svc, event, scope)
	}
	svc.OnNoticeEvent = func(ctx context.Context, event tracking.NoticeEvent) error {
		tx, ok := tracking.NoticeWriteTx(ctx)
		if !ok {
			return notification.ErrUnavailable
		}
		return inbox.EnqueueEventTx(ctx, tx, event.ID, event.OccurredAt)
	}
	// Tracking is configured once at startup, before requests or workers start.
	svc.NoticeRules.BrowseEnabled = os.Getenv("BROWSE_NOTIFICATIONS_ENABLED") == "true"
	cfg := notification.ConsumerConfig{Channel: "official_account", PollInterval: 2 * time.Second, LeaseDuration: time.Minute, ErrorBackoff: 10 * time.Second, MaxAttempts: 5, Eligibility: eligible}
	deliveryReady := bindings != nil && officialCfg.Status().BindingAvailable && officialCfg.Status().DeliveryAvailable && fieldsErr == nil
	if deliveryReady {
		cfg.Binding = func(ctx context.Context, recipient int64) (int64, bool, error) {
			b, err := bindings.ResolveBinding(ctx, recipient, officialCfg.AppID)
			if errors.Is(err, officialaccount.ErrNotBound) || errors.Is(err, officialaccount.ErrNotSubscribed) {
				return 0, false, nil
			}
			if err != nil {
				return 0, false, err
			}
			return b.Version, true, nil
		}
	}
	consumer, err := notification.NewConsumer(inbox, cfg)
	if err != nil {
		svc.NoticeRules.BrowseEnabled = false
		log.Printf("[notification] error consumer_disabled error_type=%T", err)
	} else {
		start(consumer.Run)
	}
	if deliveryReady {
		sender := officialaccount.NewSender(officialCfg, bindings, newTrackingWechatAPI(miniprogramCredentials()))
		send := officialaccount.DeliveryAdapter(sender, func(_ context.Context, job notification.DeliveryJob) (officialaccount.SendRequest, error) {
			data := map[string]string{}
			values := map[string]string{"title": job.Notice.Title, "body": job.Notice.Body, "time": job.Notice.CreatedAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")}
			for key, source := range fields {
				value := []rune(values[source])
				if len(value) > 200 {
					value = value[:200]
				}
				data[key] = string(value)
			}
			return officialaccount.SendRequest{DetailQuery: url.Values{"notification_id": {job.Notice.ID}}.Encode(), Data: data}, nil
		})
		worker, err := notification.NewDeliveryWorker(inbox, notification.DeliveryWorkerConfig{Channel: "official_account", PollInterval: 2 * time.Second, LeaseDuration: time.Minute, ErrorBackoff: 10 * time.Second, RetryBase: 30 * time.Second, RetryMax: 30 * time.Minute, MaxAttempts: 5, Send: send, Eligibility: func(ctx context.Context, job notification.DeliveryJob) (bool, error) {
			ok, err := eligible(ctx, deliveryNoticeEvent(job))
			if err != nil || !ok {
				return false, err
			}
			_, err = bindings.ResolveVersion(ctx, job.Delivery.RecipientID, officialCfg.AppID, job.Delivery.BindingVersion)
			if errors.Is(err, officialaccount.ErrBindingChanged) || errors.Is(err, officialaccount.ErrNotBound) || errors.Is(err, officialaccount.ErrNotSubscribed) {
				return false, nil
			}
			return err == nil, err
		}})
		if err != nil {
			log.Printf("[notification] error delivery_disabled error_type=%T", err)
		} else {
			start(worker.Run)
		}
	}
	return func() { cancel(); jobs.Wait() }
}

func deliveryNoticeEvent(job notification.DeliveryJob) notification.Event {
	return notification.Event{ID: job.Notice.EventID, Kind: job.Notice.Kind, RecipientID: job.Delivery.RecipientID}
}

func phase3NoticeEligible(ctx context.Context, svc *tracking.Service, event notification.Event, scope tracking.Scope) (bool, error) {
	if event.Owner != "" && !scope.Allows(event.Owner) {
		return false, nil
	}
	if event.Kind == "project_note_open" {
		return svc.ProjectNoteNoticeEligible(ctx, event.ID, event.RecipientID, scope.Owners...)
	}
	return svc.NoticeEligible(ctx, event.ID, event.RecipientID, scope.Owners...)
}
