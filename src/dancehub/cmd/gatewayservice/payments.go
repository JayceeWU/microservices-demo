package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	creditsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/credits/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	paymentsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payments/v1"
	recommendationsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/recommendations/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func simulatedPaymentsEnabled(environment, mode, flag string) (bool, error) {
	if flag != "true" {
		return false, nil
	}
	if mode != "fake" || (environment != "local" && environment != "test") {
		return false, fmt.Errorf("simulated payments require fake mode and local/test environment")
	}
	return true, nil
}

func (g *gateway) ownedOrder(ctx context.Context, id string) (*ordersv1.Order, error) {
	order, err := g.orderClient.GetOrder(ctx, &ordersv1.GetOrderRequest{Id: id})
	if err != nil {
		return nil, err
	}
	actor := platform.Actor(ctx)
	if actor.UserID == "" || order.UserId != actor.UserID {
		return nil, status.Error(codes.NotFound, "order not found")
	}
	return order, nil
}

func (g *gateway) payment(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 15*time.Second)
	defer cancel()
	var item *paymentsv1.Payment
	var err error
	responseStatus := http.StatusOK
	switch r.Pattern {
	case "POST /v1/payments":
		var input struct {
			OrderID        string `json:"orderId"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, 400, "invalid_payment", "Invalid payment command")
			return
		}
		order, e := g.ownedOrder(ctx, input.OrderID)
		if e != nil {
			grpcProblem(w, e)
			return
		}
		// Return an existing payment even after the order completes or the deadline
		// expires, so retries after a lost response remain safe.
		item, err = g.paymentClient.GetPaymentByOrder(ctx, &paymentsv1.GetPaymentByOrderRequest{OrderId: order.Id})
		if status.Code(err) == codes.NotFound {
			if order.Status != ordersv1.OrderStatus_ORDER_STATUS_PENDING_PAYMENT && order.Status != ordersv1.OrderStatus_ORDER_STATUS_PAYMENT_FAILED {
				grpcProblem(w, status.Error(codes.FailedPrecondition, "order is not payable"))
				return
			}
			item, err = g.paymentClient.CreatePayment(ctx, &paymentsv1.CreatePaymentRequest{OrderId: order.Id, AmountCents: order.GetTotal().GetAmountCents(), OwnerUserId: order.UserId, PaymentExpiresAt: order.PaymentExpiresAt, Audit: audit(input.IdempotencyKey, "")})
			responseStatus = http.StatusCreated
		}
	case "GET /v1/orders/{orderId}/payment":
		order, e := g.ownedOrder(ctx, r.PathValue("orderId"))
		if e != nil {
			grpcProblem(w, e)
			return
		}
		item, err = g.paymentClient.GetPaymentByOrder(ctx, &paymentsv1.GetPaymentByOrderRequest{OrderId: order.Id})
	case "GET /v1/payments/{id}", "POST /v1/payments/{id}/simulate":
		if r.Method == http.MethodPost {
			enabled, configErr := simulatedPaymentsEnabled(os.Getenv("ENVIRONMENT"), os.Getenv("PAYMENT_MODE"), os.Getenv("ENABLE_SIMULATED_PAYMENTS"))
			if configErr != nil || !enabled {
				http.NotFound(w, r)
				return
			}
		}
		item, err = g.paymentClient.GetPayment(ctx, &paymentsv1.GetPaymentRequest{Id: r.PathValue("id")})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		if _, e := g.ownedOrder(ctx, item.OrderId); e != nil {
			grpcProblem(w, e)
			return
		}
		if r.Method == http.MethodPost {
			var input struct {
				Outcome        string `json:"outcome"`
				IdempotencyKey string `json:"idempotencyKey"`
			}
			if platform.DecodeJSON(r, &input) != nil {
				platform.Problem(w, 400, "invalid_payment", "Invalid simulation command")
				return
			}
			item, err = g.paymentClient.SimulatePayment(ctx, &paymentsv1.SimulatePaymentRequest{Id: item.Id, Outcome: input.Outcome, Audit: audit(input.IdempotencyKey, "")})
		}
	}
	if err != nil {
		grpcProblem(w, err)
		return
	}
	platform.JSON(w, responseStatus, paymentJSON(item))
}
func (g *gateway) insights(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 3*time.Second)
	defer cancel()
	response, err := g.recommendationClient.GetStudioAnalytics(ctx, &recommendationsv1.GetStudioAnalyticsRequest{StudioId: r.URL.Query().Get("studio_id"), Month: r.URL.Query().Get("month")})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	metrics := make([]map[string]any, 0, len(response.Metrics))
	for _, metric := range response.Metrics {
		metrics = append(metrics, map[string]any{"name": metric.Name, "value": metric.Value})
	}
	platform.JSON(w, 200, map[string]any{"metrics": metrics})
}
func (g *gateway) credit(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	switch r.Pattern {
	case "GET /v1/credits/balances":
		response, err := g.creditClient.GetBalances(ctx, &creditsv1.GetBalancesRequest{})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Balances))
		for _, item := range response.Balances {
			items = append(items, map[string]any{"studioId": item.StudioId, "availableCredits": item.GetAvailable().GetUnits(), "unlimitedActive": item.UnlimitedActive})
		}
		platform.JSON(w, http.StatusOK, map[string]any{"balances": items})
	case "GET /v1/credits/grants":
		response, err := g.creditClient.ListGrants(ctx, &creditsv1.ListGrantsRequest{Page: &commonv1.PageRequest{PageSize: queryPageSize(r)}})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Grants))
		for _, item := range response.Grants {
			value := map[string]any{"id": item.Id, "userId": item.UserId, "studioId": item.StudioId, "remainingCredits": item.GetRemaining().GetUnits(), "unlimited": item.Unlimited, "status": strings.TrimPrefix(item.Status.String(), "GRANT_STATUS_"), "remainingValiditySeconds": item.RemainingValiditySeconds, "finalSale": item.FinalSale}
			if item.ExpiresAt != nil {
				value["expiresAt"] = item.ExpiresAt.AsTime()
			}
			if item.PausedAt != nil {
				value["pausedAt"] = item.PausedAt.AsTime()
			}
			items = append(items, value)
		}
		platform.JSON(w, http.StatusOK, map[string]any{"grants": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "GET /v1/credits/ledger":
		response, err := g.creditClient.ListLedger(ctx, &creditsv1.ListLedgerRequest{Page: &commonv1.PageRequest{PageSize: queryPageSize(r)}})
		if err != nil {
			grpcProblem(w, err)
			return
		}
		items := make([]map[string]any, 0, len(response.Entries))
		for _, item := range response.Entries {
			items = append(items, map[string]any{"id": item.Id, "studioId": item.StudioId, "grantId": item.GrantId, "bookingId": item.BookingId, "kind": strings.TrimPrefix(item.Kind.String(), "LEDGER_KIND_"), "creditDelta": item.CreditDelta, "reason": item.Reason, "createdAt": item.CreatedAt.AsTime()})
		}
		platform.JSON(w, http.StatusOK, map[string]any{"entries": items, "nextPageToken": response.GetPage().GetNextPageToken()})
	case "POST /v1/credits/grants/{grantId}/transfer", "POST /v1/credits/grants/{grantId}/pause", "POST /v1/credits/grants/{grantId}/resume":
		var input struct {
			TargetUserID   string `json:"targetUserId"`
			Reason         string `json:"reason"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if platform.DecodeJSON(r, &input) != nil {
			platform.Problem(w, http.StatusBadRequest, "invalid_credit_command", "Invalid credit command")
			return
		}
		var result *creditsv1.GrantOperationResponse
		var err error
		grantID := r.PathValue("grantId")
		switch r.Pattern {
		case "POST /v1/credits/grants/{grantId}/transfer":
			result, err = g.creditClient.TransferGrant(ctx, &creditsv1.TransferGrantRequest{GrantId: grantID, TargetUserId: input.TargetUserID, Audit: audit(input.IdempotencyKey, input.Reason)})
		case "POST /v1/credits/grants/{grantId}/pause":
			result, err = g.creditClient.PauseGrant(ctx, &creditsv1.PauseGrantRequest{GrantId: grantID, Audit: audit(input.IdempotencyKey, input.Reason)})
		case "POST /v1/credits/grants/{grantId}/resume":
			result, err = g.creditClient.ResumeGrant(ctx, &creditsv1.ResumeGrantRequest{GrantId: grantID, Audit: audit(input.IdempotencyKey, input.Reason)})
		}
		if err != nil {
			grpcProblem(w, err)
			return
		}
		value := map[string]any{"operation": result.Operation, "grantId": result.GrantId, "successorGrantId": result.SuccessorGrantId, "status": result.Status}
		if result.ExpiresAt != nil {
			value["expiresAt"] = result.ExpiresAt.AsTime()
		}
		platform.JSON(w, http.StatusOK, value)
	}
}
