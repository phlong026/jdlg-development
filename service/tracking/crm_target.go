package tracking

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// CRMTarget is a verified internal object identifier, never a caller-controlled URL.
// The destination CRM detail endpoint must authenticate and authorize again.
type CRMTarget struct {
	Kind           string `json:"target_kind"`
	CustomerID     string `json:"customer_id"`
	BusinessNumber string `json:"business_number"`
	Owner          string `json:"owner_id"`
}

func (s *Service) VisitorCRMTarget(ctx context.Context, scope Scope, visitor string) (CRMTarget, error) {
	var out CRMTarget
	if err := s.authorizeView(scope); err != nil {
		return out, err
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(visitor, "u:"), 10, 64)
	if !scope.CRM || !strings.HasPrefix(visitor, "u:") || err != nil || id <= 0 || visitor != "u:"+strconv.FormatInt(id, 10) {
		return out, fail(403, "需要客户映射权限及已识别账号")
	}
	if s.VerifyCRM == nil {
		return out, fail(503, "客户关系验证尚未接通")
	}
	var owner, customer, number string
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		dead, e := tombstoned(r, "deletion", visitor)
		if e != nil {
			return e
		}
		if dead {
			return fail(410, "访问记录已删除")
		}
		owner, e = privateOwner(r, scope, visitor)
		if e != nil {
			return e
		}
		row, e := r.Get("crm", privateKey(owner, visitor))
		if e != nil {
			return e
		}
		if row.Owner != owner || row.Visitor != visitor {
			return fail(404, "没有可访问的客户关联")
		}
		var link map[string]string
		if json.Unmarshal(row.Payload, &link) != nil {
			return ErrUnavailable
		}
		customer, number = link["customer_id"], link["business_number"]
		if link["source"] != "verified_upstream" || !trim(customer, 128) || strings.ContainsAny(customer, "/\\%;\r\n") || customer == "." || customer == ".." || !trim(number, 100) {
			return fail(409, "客户关联需要重新核实")
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	// The CRM bridge is an external HTTP call. It runs after the snapshot closes
	// so no transaction or pool connection is held while it answers.
	verifyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err = s.VerifyCRM(verifyCtx, ownerScope(scope, owner), visitor, customer, number); err != nil {
		return out, err
	}
	return CRMTarget{Kind: "customer", CustomerID: customer, BusinessNumber: number, Owner: owner}, nil
}
