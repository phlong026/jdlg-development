package route

import (
	"database/sql"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"service/function"
	"service/model"
	"service/report"
)

func TestValidateReportWechatIdentity(t *testing.T) {
	user := &model.User{ID: 7, OpenID: "openid-7", UnionID: "union-7"}
	valid := &function.WechatCode2SessionResponse{OpenID: "openid-7", UnionID: "union-7"}
	if err := validateReportWechatIdentity(user, valid); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}

	tests := []struct {
		name    string
		user    *model.User
		session *function.WechatCode2SessionResponse
	}{
		{name: "openid mismatch", user: user, session: &function.WechatCode2SessionResponse{OpenID: "attacker", UnionID: "union-7"}},
		{name: "session unionid missing", user: user, session: &function.WechatCode2SessionResponse{OpenID: "openid-7"}},
		{name: "stored unionid missing", user: &model.User{ID: 7, OpenID: "openid-7"}, session: valid},
		{name: "unionid mismatch", user: user, session: &function.WechatCode2SessionResponse{OpenID: "openid-7", UnionID: "other"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateReportWechatIdentity(test.user, test.session); err == nil {
				t.Fatal("unsafe identity was accepted")
			}
		})
	}
}

func TestBuildReportVirtualPayOrderUsesFrozenReservation(t *testing.T) {
	reservation := report.PaymentReservation{
		OrderNo: "VP-REPORT-1", ProductID: "report-full-v1", PriceFen: 990,
		PackID: "pack-a", PackVersion: "2026-09",
	}
	got, err := buildReportVirtualPayOrder(7, "openid-7", "report-abc", reservation)
	if err != nil {
		t.Fatalf("build report order: %v", err)
	}
	if got.OrderNo != reservation.OrderNo || got.ProductID != reservation.ProductID || got.AmountFen != reservation.PriceFen {
		t.Fatalf("order did not use frozen reservation: %#v", got)
	}
	if got.Type != reportPlanType || got.PayMode != "short_series_goods" || got.Description != "report:report-abc" {
		t.Fatalf("unexpected report order metadata: %#v", got)
	}
}

func TestValidateExistingReportOrderRejectsMismatch(t *testing.T) {
	reservation := report.PaymentReservation{OrderNo: "VP-1", ProductID: "report-full", PriceFen: 990, PackID: "pack", PackVersion: "v1"}
	base := model.UserOrder{
		UserID: 7, OpenID: "openid-7", OrderNo: "VP-1", Type: reportPlanType,
		OrderCategory: reportPlanType, Status: "pending", PayChannel: "virtualpay",
		PayMode: "short_series_goods", ProductID: "report-full", AmountFen: 990,
		Description: "report:report-1",
	}
	if err := validateExistingReportOrder(&base, 7, "openid-7", "report-1", reservation); err != nil {
		t.Fatalf("matching order rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*model.UserOrder)
	}{
		{name: "owner", edit: func(o *model.UserOrder) { o.UserID = 8 }},
		{name: "openid", edit: func(o *model.UserOrder) { o.OpenID = "attacker" }},
		{name: "report", edit: func(o *model.UserOrder) { o.Description = "report:other" }},
		{name: "product", edit: func(o *model.UserOrder) { o.ProductID = "cheap" }},
		{name: "amount", edit: func(o *model.UserOrder) { o.AmountFen = 1 }},
		{name: "failed", edit: func(o *model.UserOrder) { o.Status = "failed" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order := base
			test.edit(&order)
			if err := validateExistingReportOrder(&order, 7, "openid-7", "report-1", reservation); err == nil {
				t.Fatal("mismatched order was accepted")
			}
		})
	}
}

func TestValidatePaidReportOrderRequiresFrozenFields(t *testing.T) {
	reservation := report.PaymentReservation{OrderNo: "VP-1", ProductID: "report-full", PriceFen: 990, PackID: "pack", PackVersion: "v1"}
	order := &model.UserOrder{
		UserID: 7, OrderNo: "VP-1", Type: reportPlanType, OrderCategory: reportPlanType,
		PayChannel: "virtualpay", PayMode: "short_series_goods", ProductID: "report-full",
		AmountFen: 990, Description: "report:report-1",
	}
	if err := validatePaidReportOrder(order, "report-1", reservation); err != nil {
		t.Fatalf("matching paid order rejected: %v", err)
	}
	order.AmountFen = 1
	if err := validatePaidReportOrder(order, "report-1", reservation); err == nil {
		t.Fatal("tampered payment amount was accepted")
	}
}

func TestEnsureReportVirtualPayOrderRecoversDuplicateInsert(t *testing.T) {
	reservation := report.PaymentReservation{OrderNo: "VP-1", ProductID: "report-full", PriceFen: 990, PackID: "pack", PackVersion: "v1"}
	input, err := buildReportVirtualPayOrder(7, "openid-7", "report-1", reservation)
	if err != nil {
		t.Fatal(err)
	}
	store := &duplicateReportOrderStore{order: &model.UserOrder{
		UserID: 7, OpenID: "openid-7", OrderNo: "VP-1", Type: reportPlanType,
		OrderCategory: reportPlanType, Status: "pending", PayChannel: "virtualpay",
		PayMode: "short_series_goods", ProductID: "report-full", AmountFen: 990,
		Description: "report:report-1",
	}}
	if err := ensureReportVirtualPayOrder(store, input, "report-1", reservation); err != nil {
		t.Fatalf("duplicate insert should converge on matching order: %v", err)
	}
	if store.getCalls != 2 || store.createCalls != 1 {
		t.Fatalf("unexpected database calls: get=%d create=%d", store.getCalls, store.createCalls)
	}
}

type duplicateReportOrderStore struct {
	order       *model.UserOrder
	getCalls    int
	createCalls int
}

func (s *duplicateReportOrderStore) GetByOrderNo(string) (*model.UserOrder, error) {
	s.getCalls++
	if s.getCalls == 1 {
		return nil, sql.ErrNoRows
	}
	return s.order, nil
}

func (s *duplicateReportOrderStore) CreateVirtualPayOrder(*model.VirtualPayOrderInput) error {
	s.createCalls++
	return &mysqlDriver.MySQLError{Number: 1062, Message: "duplicate"}
}
