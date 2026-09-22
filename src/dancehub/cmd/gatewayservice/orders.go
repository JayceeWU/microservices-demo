package main

import (
	"net/http"
	"strings"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	payrollv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payroll/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
)

func (g *gateway) orders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 15*time.Second)
	defer cancel()
	var input struct {
		Items []struct {
			ProductVersionID string `json:"productVersionId"`
			Quantity         int32  `json:"quantity"`
		} `json:"items"`
		RoomReservationID string `json:"roomReservationId"`
		Reason            string `json:"reason"`
		IdempotencyKey    string `json:"idempotencyKey"`
	}
	if r.Method != http.MethodGet && platform.DecodeJSON(r, &input) != nil {
		platform.Problem(w, http.StatusBadRequest, "invalid_order_command", "Invalid order command")
		return
	}
	var result *ordersv1.Order
	var err error
	statusCode := http.StatusOK
	switch r.Pattern {
	case "POST /v1/orders":
		items := make([]*ordersv1.OrderItemInput, 0, len(input.Items))
		for _, item := range input.Items {
			items = append(items, &ordersv1.OrderItemInput{ProductVersionId: item.ProductVersionID, Quantity: item.Quantity})
		}
		result, err = g.orderClient.CreateOrderFromCart(ctx, &ordersv1.CreateOrderFromCartRequest{Items: items, Audit: audit(input.IdempotencyKey, input.Reason)})
		statusCode = http.StatusCreated
	case "POST /v1/orders/room":
		result, err = g.orderClient.CreateRoomOrder(ctx, &ordersv1.CreateRoomOrderRequest{RoomReservationId: input.RoomReservationID, Audit: audit(input.IdempotencyKey, input.Reason)})
		statusCode = http.StatusCreated
	case "GET /v1/orders/{id}":
		result, err = g.orderClient.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: r.PathValue("id")})
	case "POST /v1/orders/{id}/refund":
		result, err = g.orderClient.RequestRefund(ctx, &ordersv1.RequestRefundRequest{Id: r.PathValue("id"), Audit: audit(input.IdempotencyKey, input.Reason)})
		statusCode = http.StatusAccepted
	case "POST /v1/orders/{id}/refund/approve":
		result, err = g.orderClient.ApproveRefund(ctx, &ordersv1.ApproveRefundRequest{Id: r.PathValue("id"), Audit: audit(input.IdempotencyKey, input.Reason)})
	case "POST /v1/orders/{id}/refund/reject":
		result, err = g.orderClient.RejectRefund(ctx, &ordersv1.RejectRefundRequest{Id: r.PathValue("id"), Audit: audit(input.IdempotencyKey, input.Reason)})
	case "POST /v1/room-reservations/{roomReservationId}/cancel":
		result, err = g.orderClient.CancelRoomReservation(ctx, &ordersv1.CancelRoomReservationRequest{RoomReservationId: r.PathValue("roomReservationId"), Audit: audit(input.IdempotencyKey, input.Reason)})
	case "POST /v1/flash-sales/{campaignId}/reservations":
		var request *ordersv1.FlashSaleRequest
		request, err = g.orderClient.ReserveFlashSalePurchase(ctx, &ordersv1.ReserveFlashSalePurchaseRequest{CampaignId: r.PathValue("campaignId"), Audit: audit(input.IdempotencyKey, input.Reason)})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		platform.JSON(w, http.StatusAccepted, map[string]any{"requestId": request.RequestId, "campaignId": request.CampaignId, "status": strings.TrimPrefix(request.Status.String(), "FLASH_SALE_STATUS_")})
		return
	case "GET /v1/flash-sale-requests/{requestId}":
		request, e := g.orderClient.GetFlashSaleRequest(ctx, &ordersv1.GetFlashSaleRequestRequest{RequestId: r.PathValue("requestId")})
		if e != nil {
			grpcProblem(w, e)
			return
		}
		platform.JSON(w, http.StatusOK, map[string]any{"requestId": request.RequestId, "campaignId": request.CampaignId, "status": strings.TrimPrefix(request.Status.String(), "FLASH_SALE_STATUS_"), "orderId": request.OrderId})
		return
	}
	if err != nil {
		grpcProblem(w, err)
		return
	}
	platform.JSON(w, statusCode, orderJSON(result))
}
func (g *gateway) payroll(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 3*time.Second)
	defer cancel()
	response, err := g.payrollClient.GetMonthlyPayroll(ctx, &payrollv1.GetMonthlyPayrollRequest{StudioId: r.URL.Query().Get("studio_id"), TeacherId: r.URL.Query().Get("teacher_id"), Month: r.URL.Query().Get("month"), Page: &commonv1.PageRequest{PageSize: queryPageSize(r)}})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	lines := make([]map[string]any, 0, len(response.Lines))
	for _, line := range response.Lines {
		lines = append(lines, map[string]any{"classSessionId": line.ClassSessionId, "teacherId": line.TeacherId, "approvedDurationMinutes": line.ApprovedDurationMinutes, "redemptionCount": line.RedemptionCount, "baseAmountCents": line.BaseAmountCents, "attendanceAmountCents": line.AttendanceAmountCents, "totalAmountCents": line.TotalAmountCents, "currency": "USD"})
	}
	platform.JSON(w, http.StatusOK, map[string]any{"studioId": r.URL.Query().Get("studio_id"), "month": r.URL.Query().Get("month"), "lines": lines, "totalAmountCents": response.TotalAmountCents, "currency": "USD", "nextPageToken": response.GetPage().GetNextPageToken()})
}
