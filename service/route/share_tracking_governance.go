package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"service/tracking"
)

// makeTrackingCRMVerifier verifies the target through the existing trusted CRM
// bridge under this actor's active linked account. This does not claim that
// an account is a real-world person: that association remains an explicit,
// permissioned operator decision with its basis stored by LinkCRM.
func makeTrackingCRMVerifier(db *sql.DB) func(context.Context, tracking.Scope, string, string, string) error {
	return func(ctx context.Context, scope tracking.Scope, visitor, customer, businessNumber string) error {
		denied := func() error { return &tracking.Error{Status: 403, Message: "CRM客户不可关联或验证失败"} }
		if db == nil || scope.Actor <= 0 || scope.Tenant == "" || len(scope.Owners) != 1 || !scope.CRM || !scope.View || !safeCRMID(customer) || strings.TrimSpace(businessNumber) == "" || !strings.HasPrefix(visitor, "u:") {
			return denied()
		}
		userID, e := strconv.ParseInt(strings.TrimPrefix(visitor, "u:"), 10, 64)
		if e != nil || userID <= 0 {
			return denied()
		}
		access, e := readManagementAccess(ctx, db, scope.Actor)
		if e != nil || !access.Enabled || (access.Scope != "platform_admin" && access.Scope != "crm_only") {
			return denied()
		}
		var owner string
		e = db.QueryRowContext(ctx, `SELECT owner_id FROM tracking_access WHERE user_id=? AND tenant_id=? AND owner_id=? AND enabled=TRUE AND can_view=TRUE AND can_crm=TRUE`, scope.Actor, scope.Tenant, scope.Owners[0]).Scan(&owner)
		if e != nil || owner != scope.Owners[0] {
			return denied()
		}
		var subject string
		e = db.QueryRowContext(ctx, `SELECT crm_username FROM management_crm_link WHERE user_id=? AND enabled=TRUE AND state='active'`, scope.Actor).Scan(&subject)
		if e != nil || strings.TrimSpace(subject) == "" || subject == "@platform" {
			return denied()
		}
		status, data, _, e := crmRemote(ctx, http.MethodGet, "/customers/"+url.PathEscape(customer), subject, scope.Actor, nil, nil)
		if e != nil || status != http.StatusOK {
			return denied()
		}
		if !validTrackingCRMTarget(data, customer, businessNumber) {
			return denied()
		}
		return nil
	}
}
func validTrackingCRMTarget(data []byte, customer, businessNumber string) bool {
	var response struct {
		Code *int `json:"code"`
		Data struct {
			Customer struct {
				ID         string  `json:"id"`
				Number     string  `json:"customerNumber"`
				MergedInto *string `json:"mergedInto"`
				Lifecycle  string  `json:"lifecycle"`
			} `json:"customer"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &response) != nil || response.Code == nil || (*response.Code != 0 && *response.Code != 200) {
		return false
	}
	c := response.Data.Customer
	return c.ID == customer && c.Number != "" && c.Number == businessNumber && (c.MergedInto == nil || *c.MergedInto == "") && c.Lifecycle != "deleted" && c.Lifecycle != "merged"
}
