package route

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"service/function"
	"service/model"
	"service/report"
)

const (
	reportPlanType       = "report"
	reportOrderPrefix    = "report:"
	reportPaymentTimeout = 10 * time.Second
)

// V5 delivery is independent of enterprise contact linking. OpenID is still
// verified by code2session against the authenticated report owner's account.
func validateReportProtocolIdentity(user *model.User, session *function.WechatCode2SessionResponse, protocol string) error {
	if protocol != report.ProtocolDecisionV5 {
		return validateReportWechatIdentity(user, session)
	}
	if user == nil || session == nil || strings.TrimSpace(user.OpenID) == "" || user.OpenID != session.OpenID {
		return errors.New("report payment: WeChat identity mismatch")
	}
	return nil
}

func validateReportWechatIdentity(user *model.User, session *function.WechatCode2SessionResponse) error {
	if user == nil || session == nil || strings.TrimSpace(user.OpenID) == "" || user.OpenID != session.OpenID {
		return errors.New("report payment: WeChat identity mismatch")
	}
	if strings.TrimSpace(user.UnionID) == "" || strings.TrimSpace(session.UnionID) == "" || user.UnionID != session.UnionID {
		return errors.New("report payment: verified UnionID required")
	}
	return nil
}

func buildReportVirtualPayOrder(userID int64, openID, reportID string, reservation report.PaymentReservation) (*model.VirtualPayOrderInput, error) {
	if userID <= 0 || strings.TrimSpace(openID) == "" || !validReportPaymentReservation(reservation) || !validReportID(reportID) {
		return nil, errors.New("report payment: invalid frozen order")
	}
	return &model.VirtualPayOrderInput{
		UserID:        userID,
		OpenID:        openID,
		OrderNo:       reservation.OrderNo,
		Type:          reportPlanType,
		OrderCategory: reportPlanType,
		Amount:        0,
		AmountFen:     reservation.PriceFen,
		Title:         "完整建房报告",
		Description:   reportOrderPrefix + reportID,
		PayMode:       "short_series_goods",
		ProductID:     reservation.ProductID,
	}, nil
}

type reportVirtualPayOrderStore interface {
	GetByOrderNo(orderNo string) (*model.UserOrder, error)
	CreateVirtualPayOrder(input *model.VirtualPayOrderInput) error
}

func ensureReportVirtualPayOrder(orderModel reportVirtualPayOrderStore, input *model.VirtualPayOrderInput, reportID string, reservation report.PaymentReservation) error {
	if orderModel == nil || input == nil {
		return errors.New("report payment: order model unavailable")
	}
	existing, err := orderModel.GetByOrderNo(input.OrderNo)
	if err == nil {
		return validateExistingReportOrder(existing, input.UserID, input.OpenID, reportID, reservation)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("report payment: read pending order: %w", err)
	}
	if err := orderModel.CreateVirtualPayOrder(input); err == nil {
		return nil
	} else if !isDuplicateVirtualPayOrder(err) {
		return fmt.Errorf("report payment: create pending order: %w", err)
	}
	existing, err = orderModel.GetByOrderNo(input.OrderNo)
	if err != nil {
		return fmt.Errorf("report payment: read duplicate order: %w", err)
	}
	return validateExistingReportOrder(existing, input.UserID, input.OpenID, reportID, reservation)
}

func validateExistingReportOrder(order *model.UserOrder, userID int64, openID, reportID string, reservation report.PaymentReservation) error {
	if err := validatePaidReportOrder(order, reportID, reservation); err != nil {
		return err
	}
	if order.UserID != userID || order.OpenID != openID || order.Status != "pending" {
		return errors.New("report payment: pending order does not match current user")
	}
	return nil
}

func validatePaidReportOrder(order *model.UserOrder, reportID string, reservation report.PaymentReservation) error {
	if order == nil || !validReportPaymentReservation(reservation) || !validReportID(reportID) {
		return errors.New("report payment: invalid order verification input")
	}
	if order.OrderNo != reservation.OrderNo || order.Type != reportPlanType || order.OrderCategory != reportPlanType ||
		order.PayChannel != "virtualpay" || order.PayMode != "short_series_goods" ||
		order.ProductID != reservation.ProductID || order.AmountFen != reservation.PriceFen ||
		order.Description != reportOrderPrefix+reportID {
		return errors.New("report payment: paid order differs from frozen reservation")
	}
	return nil
}

func deliverPaidReport(deps VirtualPayDeps, order *model.UserOrder) error {
	if deps.ReportService == nil || order == nil {
		return errors.New("report payment: report service unavailable")
	}
	reportID := reportIDFromDescription(order.Description)
	if reportID == "" {
		return errors.New("report payment: report id missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), reportPaymentTimeout)
	defer cancel()
	reservation, err := deps.ReportService.GetPaymentReservation(ctx, order.UserID, reportID)
	if err != nil {
		return fmt.Errorf("report payment: read reservation: %w", err)
	}
	if err := validatePaidReportOrder(order, reportID, reservation); err != nil {
		return err
	}
	if _, err := deps.ReportService.AuthorizeVerifiedPayment(ctx, order.UserID, reportID, order.OrderNo); err != nil {
		return fmt.Errorf("report payment: authorize payment: %w", err)
	}
	return nil
}

func requireReportReady(deps VirtualPayDeps, meta *model.VirtualPayOrderMeta) error {
	if deps.ReportService == nil || deps.UserOrderModel == nil || meta == nil {
		return errors.New("report payment: report dependencies unavailable")
	}
	order, err := deps.UserOrderModel.GetByOrderNo(meta.OrderNo)
	if err != nil {
		return fmt.Errorf("report payment: read order: %w", err)
	}
	reportID := reportIDFromDescription(order.Description)
	if reportID == "" || order.UserID != meta.UserID {
		return errors.New("report payment: invalid report order")
	}
	ctx, cancel := context.WithTimeout(context.Background(), reportPaymentTimeout)
	defer cancel()
	view, err := deps.ReportService.Get(ctx, order.UserID, reportID)
	if err != nil {
		return fmt.Errorf("report payment: read report: %w", err)
	}
	if view.Status != report.StatusReady {
		return report.ErrNotReady
	}
	return nil
}

func reportIDFromDescription(description string) string {
	if !strings.HasPrefix(description, reportOrderPrefix) {
		return ""
	}
	id := strings.TrimPrefix(description, reportOrderPrefix)
	if !validReportID(id) {
		return ""
	}
	return id
}

func validReportID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 {
		return false
	}
	for _, char := range id {
		if !(char == '-' || char == '_' || char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z') {
			return false
		}
	}
	return true
}

func validReportPaymentReservation(reservation report.PaymentReservation) bool {
	return strings.TrimSpace(reservation.OrderNo) != "" && strings.TrimSpace(reservation.ProductID) != "" &&
		reservation.PriceFen > 0 && strings.TrimSpace(reservation.PackID) != "" && strings.TrimSpace(reservation.PackVersion) != ""
}

func isDuplicateVirtualPayOrder(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
