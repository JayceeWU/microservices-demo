package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	cartv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/cart/v1"
	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	paymentsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payments/v1"
	schedulingv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/scheduling/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func grpcRequestContext(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(platform.OutgoingGRPCContext(r.Context()), timeout)
}

func queryPageSize(r *http.Request) int32 {
	value, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if value < 1 || value > 100 {
		return 24
	}
	return int32(value)
}

func audit(key, reason string) *commonv1.AuditContext {
	return &commonv1.AuditContext{IdempotencyKey: key, Reason: reason}
}

func profileJSON(item *accountv1.Profile) map[string]any {
	return map[string]any{"id": item.Id, "email": item.Email, "displayName": item.DisplayName, "avatarUrl": item.AvatarUrl, "timezone": item.Timezone, "bio": item.Bio, "portfolioUrl": item.PortfolioUrl}
}
func membershipJSON(item *accountv1.Membership, studioName string) map[string]any {
	return map[string]any{"id": item.Id, "studioId": item.StudioId, "userId": item.UserId, "role": accountRoleText(item.Role), "active": item.Active, "studioName": studioName}
}
func platformUserJSON(item *accountv1.PlatformUser) map[string]any {
	return map[string]any{"id": item.GetId(), "email": item.GetEmail(), "displayName": item.GetDisplayName()}
}
func globalRoleAssignmentJSON(item *accountv1.GlobalRoleAssignment) map[string]any {
	value := map[string]any{"id": item.GetId(), "user": platformUserJSON(item.GetUser()), "role": globalRoleText(item.GetRole()), "active": item.GetActive()}
	if item.GetGrantedAt() != nil {
		value["grantedAt"] = item.GetGrantedAt().AsTime()
	}
	if item.GetRevokedAt() != nil {
		value["revokedAt"] = item.GetRevokedAt().AsTime()
	}
	return value
}
func studioJSON(item *catalogv1.Studio) map[string]any {
	return map[string]any{"id": item.Id, "slug": item.Slug, "name": item.Name, "description": item.Description, "addressLine": item.AddressLine, "city": item.City, "state": item.State, "postalCode": item.PostalCode, "timezone": item.Timezone, "imageUrl": item.ImageUrl}
}
func roomJSON(item *catalogv1.Room) map[string]any {
	return map[string]any{"id": item.Id, "studioId": item.StudioId, "name": item.Name, "capacity": item.Capacity, "rentalRateCentsPerHour": item.RentalRateCentsPerHour, "facilities": item.Facilities, "imageUrl": item.ImageUrl}
}
func productJSON(item *catalogv1.CreditProductVersion) map[string]any {
	return map[string]any{"id": item.Id, "productId": item.ProductId, "studioId": item.StudioId, "issuerScope": strings.TrimPrefix(item.IssuerScope.String(), "ISSUER_SCOPE_"), "kind": strings.TrimPrefix(item.Kind.String(), "PRODUCT_KIND_"), "name": item.Name, "amountCents": item.AmountCents, "creditAmount": item.CreditAmount, "validityDays": item.ValidityDays, "finalSale": item.FinalSale, "currency": "USD"}
}
func sessionJSON(item *schedulingv1.ClassSession) map[string]any {
	return map[string]any{"id": item.Id, "studioId": item.StudioId, "roomId": item.RoomId, "teacherId": item.TeacherId, "title": item.Title, "description": item.Description, "startsAt": item.GetTimeRange().GetStartsAt().AsTime(), "endsAt": item.GetTimeRange().GetEndsAt().AsTime(), "capacity": item.Capacity, "minimumStudents": item.MinimumStudents, "creditCost": item.GetCreditCost().GetUnits(), "confirmedCount": item.ConfirmedCount, "status": strings.TrimPrefix(item.Status.String(), "CLASS_STATUS_"), "videoUrl": item.VideoUrl}
}
func bookingJSON(item *schedulingv1.Booking) map[string]any {
	return map[string]any{"id": item.Id, "studioId": item.StudioId, "classSessionId": item.ClassSessionId, "studentId": item.StudentId, "status": strings.TrimPrefix(item.Status.String(), "BOOKING_STATUS_"), "isWalkIn": item.IsWalkIn, "creditCompensationPending": item.CreditCompensationPending}
}
func bookingCollectionJSON(items []*schedulingv1.Booking, page *commonv1.PageResponse) map[string]any {
	bookings := make([]map[string]any, 0, len(items))
	for _, item := range items {
		bookings = append(bookings, bookingJSON(item))
	}
	return map[string]any{"bookings": bookings, "nextPageToken": page.GetNextPageToken()}
}
func roomReservationJSON(item *schedulingv1.RoomReservation) map[string]any {
	return map[string]any{"id": item.Id, "studioId": item.StudioId, "roomId": item.RoomId, "studentId": item.StudentId, "startsAt": item.GetTimeRange().GetStartsAt().AsTime(), "endsAt": item.GetTimeRange().GetEndsAt().AsTime(), "amountCents": item.GetAmount().GetAmountCents(), "currency": "USD", "status": strings.TrimPrefix(item.Status.String(), "ROOM_RESERVATION_STATUS_"), "holdExpiresAt": item.HoldExpiresAt.AsTime()}
}
func orderJSON(item *ordersv1.Order) map[string]any {
	value := map[string]any{"id": item.Id, "userId": item.UserId, "studioId": item.StudioId, "status": strings.TrimPrefix(item.Status.String(), "ORDER_STATUS_"), "totalAmountCents": item.GetTotal().GetAmountCents(), "currency": "USD"}
	lines := make([]map[string]any, 0, len(item.Lines))
	for _, line := range item.Lines {
		mapped := map[string]any{"id": line.Id, "description": line.Description, "unitAmountCents": line.GetUnitPrice().GetAmountCents(), "currency": "USD"}
		if product := line.GetCreditProduct(); product != nil {
			mapped["type"] = "CREDIT_PRODUCT"
			mapped["productVersionId"] = product.ProductVersionId
			mapped["quantity"] = product.Quantity
			mapped["finalSale"] = product.FinalSale
		} else if room := line.GetRoomReservation(); room != nil {
			mapped["type"] = "ROOM_RESERVATION"
			mapped["roomReservationId"] = room.RoomReservationId
		}
		lines = append(lines, mapped)
	}
	value["refundPhase"] = item.RefundPhase
	value["refundFailureReason"] = item.RefundFailureReason
	value["lines"] = lines
	if item.PaymentExpiresAt != nil {
		value["paymentExpiresAt"] = item.PaymentExpiresAt.AsTime()
	}
	return value
}
func paymentJSON(item *paymentsv1.Payment) map[string]any {
	value := map[string]any{"id": item.Id, "orderId": item.OrderId, "providerPaymentId": item.ProviderPaymentId, "amountCents": item.AmountCents, "currency": "USD", "status": item.Status, "clientSecret": item.ClientSecret, "simulationEnabled": item.SimulationEnabled}
	if item.SucceededAt != nil {
		value["succeededAt"] = item.SucceededAt.AsTime()
	}
	if item.PaymentExpiresAt != nil {
		value["paymentExpiresAt"] = item.PaymentExpiresAt.AsTime()
	}
	return value
}

func (g *gateway) browserTelemetry(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !allowedBrowserOrigin(origin) {
		platform.Problem(w, http.StatusForbidden, "telemetry_origin_denied", "Telemetry origin is not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
	suffix := strings.TrimPrefix(r.URL.Path, "/v1/telemetry")
	if suffix != "/v1/traces" && suffix != "/v1/metrics" {
		platform.Problem(w, http.StatusNotFound, "telemetry_endpoint_not_found", "Unsupported OTLP endpoint")
		return
	}
	target := strings.TrimRight(g.collectorURL, "/") + suffix
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
	if err != nil {
		platform.Problem(w, http.StatusBadRequest, "invalid_telemetry", "Unable to forward telemetry")
		return
	}
	request.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	request.Header.Set("Content-Encoding", r.Header.Get("Content-Encoding"))
	response, err := g.client.Do(request)
	if err != nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *gateway) getCart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 3*time.Second)
	defer cancel()
	cart, err := g.cart.GetCart(ctx, &cartv1.GetCartRequest{})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	writeCart(w, cart)
}

func (g *gateway) addCartItem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProductVersionID string `json:"productVersionId"`
		Quantity         int32  `json:"quantity"`
		IdempotencyKey   string `json:"idempotencyKey"`
	}
	if platform.DecodeJSON(r, &input) != nil || input.ProductVersionID == "" || input.Quantity < 1 {
		platform.Problem(w, http.StatusBadRequest, "invalid_cart_item", "productVersionId and positive quantity are required")
		return
	}
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	snapshot, err := g.catalogClient.GetProductSnapshot(ctx, &catalogv1.GetProductSnapshotRequest{ProductVersionId: input.ProductVersionID})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	cart, err := g.cart.AddItem(ctx, &cartv1.AddItemRequest{StudioId: snapshot.Version.StudioId, IssuerScope: strings.TrimPrefix(snapshot.Version.IssuerScope.String(), "ISSUER_SCOPE_"), Item: &cartv1.CartItem{ProductVersionId: input.ProductVersionID, Quantity: input.Quantity}, Audit: audit(input.IdempotencyKey, "")})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	writeCart(w, cart)
}

func (g *gateway) emptyCart(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	cart, err := g.cart.EmptyCart(ctx, &cartv1.EmptyCartRequest{Audit: audit(r.Header.Get("Idempotency-Key"), "")})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	writeCart(w, cart)
}

func (g *gateway) removeCartItem(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := grpcRequestContext(r, 10*time.Second)
	defer cancel()
	cart, err := g.cart.RemoveItem(ctx, &cartv1.RemoveItemRequest{ProductVersionId: r.PathValue("productVersionId"), Audit: audit(r.Header.Get("Idempotency-Key"), "")})
	if err != nil {
		grpcProblem(w, err)
		return
	}
	writeCart(w, cart)
}

func writeCart(w http.ResponseWriter, cart *cartv1.Cart) {
	items := make([]map[string]any, 0, len(cart.Items))
	for _, item := range cart.Items {
		items = append(items, map[string]any{"productVersionId": item.ProductVersionId, "quantity": item.Quantity})
	}
	platform.JSON(w, http.StatusOK, map[string]any{"userId": cart.UserId, "studioId": cart.StudioId, "issuerScope": cart.IssuerScope, "items": items})
}

func grpcProblem(w http.ResponseWriter, err error) {
	code := status.Code(err)
	httpStatus := http.StatusInternalServerError
	switch code {
	case codes.InvalidArgument:
		httpStatus = http.StatusBadRequest
	case codes.Unauthenticated:
		httpStatus = http.StatusUnauthorized
	case codes.PermissionDenied:
		httpStatus = http.StatusForbidden
	case codes.NotFound:
		httpStatus = http.StatusNotFound
	case codes.AlreadyExists, codes.FailedPrecondition, codes.Aborted:
		httpStatus = http.StatusConflict
	case codes.ResourceExhausted:
		httpStatus = http.StatusTooManyRequests
	case codes.Unavailable:
		httpStatus = http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		httpStatus = http.StatusGatewayTimeout
	}
	platform.Problem(w, httpStatus, "grpc_"+strings.ToLower(code.String()), status.Convert(err).Message())
}
