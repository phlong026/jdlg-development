package route

import (
	"context"
	"database/sql"
	"log"
	"service/model"
	"service/tracking"
	"strconv"
	"strings"
	"time"
)

type myCustomerDetailResponse struct {
	tracking.MyVisitorDetail
	CanConvertLead   bool                    `json:"can_convert_lead"`
	Handoff          model.MyCustomerHandoff `json:"customer_handoff"`
	ConversionReason string                  `json:"conversion_reason,omitempty"`
}

type myCustomerHandoffReader interface {
	MyCustomerHandoff(context.Context, int64, int64) (model.MyCustomerHandoff, error)
}

func presentMyCustomerHandoff(ctx context.Context, m myCustomerHandoffReader, scope tracking.Scope, detail tracking.MyVisitorDetail) (myCustomerDetailResponse, error) {
	out := myCustomerDetailResponse{MyVisitorDetail: detail}
	account, ok := trackingVisitorAccount(detail.Key)
	if !ok {
		out.ConversionReason = "请引导客户登录并在个人设置完成留资与业务联系授权"
		return out, nil
	}
	state, err := m.MyCustomerHandoff(ctx, scope.Actor, account)
	if err != nil {
		log.Printf("[Tracking] customer handoff read actor=%d failed: %v", scope.Actor, err)
		return out, tracking.ErrUnavailable
	}
	out.Handoff = state
	if state.Converted {
		return out, nil
	}
	if _, gate := trackingLeadGate(detail.State, detail.IdentityType); gate != nil {
		out.ConversionReason = gate.Message
		return out, nil
	}
	if !state.ContactReady {
		out.ConversionReason = "请引导客户在个人设置绑定手机号，并开启业务联系授权后再转为线索"
		return out, nil
	}
	out.CanConvertLead = true
	return out, nil
}

// Customer references identify mini accounts, not CRM-domain IDs. Every use
// rechecks both the active account and the actor's current scoped relationship.
func makeProjectCustomerLinkVerifier(db *sql.DB, svc *tracking.Service) func(context.Context, tracking.Scope, string) error {
	return func(ctx context.Context, scope tracking.Scope, ref string) error {
		customer, ok := trackingVisitorAccount(ref)
		if !ok || svc == nil {
			return &tracking.Error{Status: 400, Message: "请选择有效的本人客户"}
		}
		if !scope.Manage || scope.Actor <= 0 || scope.Tenant != svc.Config.Tenant || len(scope.Owners) == 0 {
			return &tracking.Error{Status: 403, Message: "无本人客户关联权限"}
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := verifyActiveMiniProgramAccount(ctx, db, customer); err != nil {
			return err
		}
		// The destination project's Manage scope never becomes customer-read rights.
		// Read the current share graph independently: forwarding company content
		// may legitimately bring a customer into this actor's personal project.
		readScope, err := loadMiniTrackingMyScope(ctx, db, scope.Tenant, scope.Actor)
		if err != nil {
			return err
		}
		return verifyMyCustomerRelationship(ctx, db, readScope, ref, customer)
	}
}

func verifyMyCustomerRelationship(ctx context.Context, db *sql.DB, scope tracking.Scope, ref string, customer int64) error {
	if !scope.View || scope.Actor <= 0 || len(scope.Owners) == 0 {
		return &tracking.Error{Status: 403, Message: "无本人客户查看权限"}
	}
	query, args := projectCustomerRelationshipQuery(scope, ref, customer)
	var allowed bool
	if err := tracking.SourceReadDB(ctx, db).QueryRowContext(ctx, query, args...).Scan(&allowed); err != nil {
		log.Printf("[Tracking] customer relationship verify actor=%d failed: %v", scope.Actor, err)
		return tracking.ErrUnavailable
	}
	if !allowed {
		return &tracking.Error{Status: 403, Message: "无法关联当前账号范围以外的客户"}
	}
	return nil
}

// These are index/EXISTS predicates, not an application scan of the latest
// 400 shares. A relationship remains verifiable when analytics moves windows.
func projectCustomerRelationshipQuery(scope tracking.Scope, ref string, customer int64) (string, []any) {
	slots := strings.TrimSuffix(strings.Repeat("?,", len(scope.Owners)), ",")
	args := []any{scope.Tenant, ref, scope.Tenant, ref, projectCustomerActorKey(scope.Actor)}
	for _, owner := range scope.Owners {
		args = append(args, owner)
	}
	args = append(args, customer, scope.Actor, scope.Tenant, projectCustomerActorKey(scope.Actor), scope.Actor)
	for _, owner := range scope.Owners {
		args = append(args, owner)
	}
	args = append(args, ref, ref)
	query := `SELECT NOT EXISTS(SELECT 1 FROM tracking_records tomb WHERE tomb.tenant_id=? AND tomb.kind='deletion' AND tomb.record_id=?) AND (
 EXISTS(SELECT 1 FROM tracking_records e JOIN tracking_records sh ON sh.tenant_id=e.tenant_id AND sh.kind='share' AND sh.owner_id=e.owner_id AND (sh.share_id=e.share_id OR (sh.share_id='' AND JSON_UNQUOTE(JSON_EXTRACT(sh.payload,'$.share_id'))=e.share_id))
 WHERE e.tenant_id=? AND e.kind='event' AND e.visitor_key=? AND JSON_UNQUOTE(JSON_EXTRACT(sh.payload,'$.sharer_id'))=? AND e.owner_id IN (` + slots + `))
 OR EXISTS(SELECT 1 FROM customer_service_events touch WHERE touch.user_id=? AND touch.event_type='share_intent_convert' AND touch.source='share_tracking_my' AND JSON_UNQUOTE(JSON_EXTRACT(touch.payload,'$.converted_by'))=?)
 OR EXISTS(SELECT 1 FROM tracking_records project WHERE project.tenant_id=? AND project.kind='note_project' AND project.visitor_key=? AND JSON_UNQUOTE(JSON_EXTRACT(project.payload,'$.designer_id'))=? AND project.owner_id IN (` + slots + `)
 AND NOT EXISTS(SELECT 1 FROM tracking_records deleted WHERE deleted.tenant_id=project.tenant_id AND deleted.kind='note_project_deletion' AND deleted.record_id=project.record_id)
 AND (JSON_UNQUOTE(JSON_EXTRACT(project.payload,'$.customer_ref'))=? OR EXISTS(SELECT 1 FROM tracking_records activity WHERE activity.tenant_id=project.tenant_id AND activity.owner_id=project.owner_id AND activity.kind IN ('note_open','note_feedback','note_confirm') AND activity.visitor_key=? AND JSON_UNQUOTE(JSON_EXTRACT(activity.payload,'$.project_id'))=project.record_id))))`
	return query, args
}

func projectCustomerActorKey(actor int64) string { return "u:" + strconv.FormatInt(actor, 10) }

type myProjectCustomerChoice struct {
	CustomerRef          string                  `json:"customer_ref"`
	Nickname             string                  `json:"nickname"`
	ConversionStatus     string                  `json:"conversion_status"`
	Handoff              model.MyCustomerHandoff `json:"customer_handoff"`
	ProjectActivityCount int                     `json:"project_activity_count"`
}

func myProjectCustomerChoices(ctx context.Context, svc *tracking.Service, m *model.CustomerServiceModel, scope tracking.Scope, f tracking.Filter) (map[string]any, error) {
	if f.Visitor != "" {
		customer, ok := trackingVisitorAccount(f.Visitor)
		if !ok {
			return nil, &tracking.Error{Status: 400, Message: "客户标识无效"}
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := verifyActiveMiniProgramAccount(ctx, m.DB, customer); err != nil {
			return nil, err
		}
		if err := verifyMyCustomerRelationship(ctx, m.DB, scope, f.Visitor, customer); err != nil {
			return nil, err
		}
		profiles, err := model.GetUserProfilePresentations(ctx, m.DB, []int64{customer})
		if err != nil {
			return nil, tracking.ErrUnavailable
		}
		state, err := m.MyCustomerHandoff(ctx, scope.Actor, customer)
		if err != nil {
			return nil, tracking.ErrUnavailable
		}
		name := strings.TrimSpace(profiles[customer].Nickname)
		if name == "" {
			name = "已识别客户"
		}
		status := "not_converted"
		if state.Converted {
			status = state.Status
		}
		return map[string]any{"items": []myProjectCustomerChoice{{CustomerRef: f.Visitor, Nickname: name, ConversionStatus: status, Handoff: state}}, "next_cursor": "", "cursor": "", "as_of": svc.Now()}, nil
	}
	list, err := svc.MyVisitors(ctx, scope, scope.Actor, f)
	if err != nil {
		return nil, err
	}
	visitors, _ := list.Items.([]tracking.MyVisitor)
	ids := []int64{}
	for _, visitor := range visitors {
		if id, ok := trackingVisitorAccount(visitor.Key); ok {
			ids = append(ids, id)
		}
	}
	states, err := m.MyCustomerHandoffs(ctx, scope.Actor, ids)
	if err != nil {
		return nil, tracking.ErrUnavailable
	}
	items := []myProjectCustomerChoice{}
	for _, visitor := range visitors {
		id, ok := trackingVisitorAccount(visitor.Key)
		if !ok {
			continue
		}
		state, active := states[id]
		if !active {
			continue
		}
		status := "not_converted"
		if state.Converted {
			status = state.Status
		}
		name := visitor.Nickname
		if name == "" {
			name = "已识别客户"
		}
		items = append(items, myProjectCustomerChoice{CustomerRef: visitor.Key, Nickname: name, ConversionStatus: status, Handoff: state, ProjectActivityCount: visitor.ProjectActivityCount})
	}
	return map[string]any{"items": items, "next_cursor": list.Cursor, "cursor": list.Cursor, "as_of": list.AsOf, "share_window": list.ShareWindow}, nil
}
