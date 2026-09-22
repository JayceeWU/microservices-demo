package grpctransport

import (
	"context"
	"errors"
	"strings"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	ordersv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/orders/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	ordersv1.UnimplementedOrderServiceServer
	service orderApplication
}

type orderApplication interface {
	CreateOrder(context.Context, application.CreateOrderCommand) (application.OrderView, error)
	CreateRoomOrder(context.Context, application.CreateRoomOrderCommand) (application.OrderView, error)
	GetOrder(context.Context, application.ActorContext, string) (application.OrderView, error)
	RequestRefund(context.Context, application.RefundCommand) (application.OrderView, error)
	ApproveRefund(context.Context, application.RefundCommand) (application.OrderView, error)
	RejectRefund(context.Context, application.RefundCommand) (application.OrderView, error)
	CancelRoomReservation(context.Context, application.CancelRoomCommand) (application.OrderView, error)
	ReserveFlashSale(context.Context, application.ReserveFlashSaleCommand) (application.FlashSaleRequestView, error)
	GetFlashSaleRequest(context.Context, application.ActorContext, string) (application.FlashSaleRequestView, error)
	ApplySaga(context.Context, application.SagaCommand) (application.OrderView, error)
}

func NewServer(service orderApplication) *Server { return &Server{service: service} }

func (s *Server) CreateOrderFromCart(ctx context.Context, request *ordersv1.CreateOrderFromCartRequest) (*ordersv1.Order, error) {
	items := make([]application.OrderItemInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, application.OrderItemInput{ProductVersionID: item.ProductVersionId, Quantity: item.Quantity})
	}
	value, err := s.service.CreateOrder(ctx, application.CreateOrderCommand{Actor: actor(ctx), Items: items, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return order(value), nil
}

func (s *Server) CreateRoomOrder(ctx context.Context, request *ordersv1.CreateRoomOrderRequest) (*ordersv1.Order, error) {
	value, err := s.service.CreateRoomOrder(ctx, application.CreateRoomOrderCommand{Actor: actor(ctx), RoomReservationID: request.RoomReservationId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return order(value), nil
}

func (s *Server) GetOrder(ctx context.Context, request *ordersv1.GetOrderRequest) (*ordersv1.Order, error) {
	value, err := s.service.GetOrder(ctx, actor(ctx), request.Id)
	if err != nil {
		return nil, grpcError(err)
	}
	return order(value), nil
}

func (s *Server) RequestRefund(ctx context.Context, request *ordersv1.RequestRefundRequest) (*ordersv1.Order, error) {
	return s.refund(ctx, request.Id, request.Audit, s.service.RequestRefund)
}
func (s *Server) ApproveRefund(ctx context.Context, request *ordersv1.ApproveRefundRequest) (*ordersv1.Order, error) {
	return s.refund(ctx, request.Id, request.Audit, s.service.ApproveRefund)
}
func (s *Server) RejectRefund(ctx context.Context, request *ordersv1.RejectRefundRequest) (*ordersv1.Order, error) {
	return s.refund(ctx, request.Id, request.Audit, s.service.RejectRefund)
}

func (s *Server) refund(ctx context.Context, id string, value *commonv1.AuditContext, execute func(context.Context, application.RefundCommand) (application.OrderView, error)) (*ordersv1.Order, error) {
	result, err := execute(ctx, application.RefundCommand{Actor: actor(ctx), OrderID: id, Audit: audit(value)})
	if err != nil {
		return nil, grpcError(err)
	}
	return order(result), nil
}

func (s *Server) CancelRoomReservation(ctx context.Context, request *ordersv1.CancelRoomReservationRequest) (*ordersv1.Order, error) {
	value, err := s.service.CancelRoomReservation(ctx, application.CancelRoomCommand{Actor: actor(ctx), RoomReservationID: request.RoomReservationId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return order(value), nil
}

func (s *Server) ReserveFlashSalePurchase(ctx context.Context, request *ordersv1.ReserveFlashSalePurchaseRequest) (*ordersv1.FlashSaleRequest, error) {
	value, err := s.service.ReserveFlashSale(ctx, application.ReserveFlashSaleCommand{Actor: actor(ctx), CampaignID: request.CampaignId, Audit: audit(request.Audit)})
	if err != nil {
		return nil, grpcError(err)
	}
	return flashSale(value), nil
}

func (s *Server) GetFlashSaleRequest(ctx context.Context, request *ordersv1.GetFlashSaleRequestRequest) (*ordersv1.FlashSaleRequest, error) {
	value, err := s.service.GetFlashSaleRequest(ctx, actor(ctx), request.RequestId)
	if err != nil {
		return nil, grpcError(err)
	}
	return flashSale(value), nil
}

func (s *Server) MarkPaymentSucceeded(ctx context.Context, request *ordersv1.MarkPaymentSucceededRequest) (*ordersv1.Order, error) {
	return s.saga(ctx, request.OrderId, request.PaymentId, request.GetAudit().GetIdempotencyKey(), domain.PaymentSucceededEvent, "payment-order-saga", request.GetPaidAt().AsTime())
}
func (s *Server) MarkPaymentFailed(ctx context.Context, request *ordersv1.MarkPaymentFailedRequest) (*ordersv1.Order, error) {
	return s.saga(ctx, request.OrderId, request.Reason, request.GetAudit().GetIdempotencyKey(), domain.PaymentFailedEvent, "payment-order-saga", time.Time{})
}
func (s *Server) saga(ctx context.Context, orderID, reason, eventID string, event domain.PaymentEvent, requiredPrincipal string, paidAt time.Time) (*ordersv1.Order, error) {
	requestActor := actor(ctx)
	if !requestActor.IsService(requiredPrincipal) {
		return nil, status.Error(codes.PermissionDenied, requiredPrincipal+" service principal required")
	}
	value, err := s.service.ApplySaga(ctx, application.SagaCommand{Actor: requestActor, OrderID: orderID, EventID: eventID, Reason: reason, Event: event, PaidAt: paidAt})
	if err != nil {
		return nil, grpcError(err)
	}
	return order(value), nil
}

func actor(ctx context.Context) application.ActorContext {
	return platform.Actor(ctx)
}
func audit(value *commonv1.AuditContext) application.AuditContext {
	if value == nil {
		return application.AuditContext{}
	}
	return application.NewAuditContext(value.IdempotencyKey, value.Reason)
}

func order(value application.OrderView) *ordersv1.Order {
	result := &ordersv1.Order{Id: value.ID, UserId: value.UserID, StudioId: value.StudioID, Status: orderStatus(value.Status), RefundPhase: value.RefundPhase, RefundFailureReason: value.RefundFailureReason, Total: &commonv1.Money{AmountCents: value.TotalCents}, PaymentExpiresAt: timestamppb.New(value.PaymentExpiresAt)}
	for _, item := range value.Lines {
		line := &ordersv1.OrderLine{Id: item.ID, Description: item.Description, UnitPrice: &commonv1.Money{AmountCents: item.UnitAmountCents}}
		if item.Type == "CREDIT_PRODUCT" {
			line.Line = &ordersv1.OrderLine_CreditProduct{CreditProduct: &ordersv1.CreditProductOrderLine{ProductVersionId: item.ProductVersionID, Quantity: item.Quantity, FinalSale: item.FinalSale}}
		} else {
			line.Line = &ordersv1.OrderLine_RoomReservation{RoomReservation: &ordersv1.RoomReservationOrderLine{RoomReservationId: item.RoomReservationID}}
		}
		result.Lines = append(result.Lines, line)
	}
	return result
}

func flashSale(value application.FlashSaleRequestView) *ordersv1.FlashSaleRequest {
	return &ordersv1.FlashSaleRequest{RequestId: value.RequestID, CampaignId: value.CampaignID, OrderId: value.OrderID, Status: map[application.FlashSaleStatus]ordersv1.FlashSaleStatus{application.FlashSaleQueued: 1, application.FlashSaleOrderCreated: 2, application.FlashSaleRejected: 3, application.FlashSaleExpired: 4}[value.Status]}
}

func orderStatus(value domain.Status) ordersv1.OrderStatus {
	return map[domain.Status]ordersv1.OrderStatus{domain.Draft: 1, domain.PendingPayment: 2, domain.Paid: 3, domain.PaidNotFulfilled: 4, domain.Fulfilled: 5, domain.PaymentFailed: 6, domain.Expired: 7, domain.RefundPending: 8, domain.Refunded: 9}[value]
}

func grpcError(err error) error {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, clean(err, application.ErrUnauthenticated))
	case errors.Is(err, application.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, clean(err, application.ErrPermissionDenied))
	case errors.Is(err, application.ErrInvalidArgument), errors.Is(err, domain.ErrInvalidOrder):
		return status.Error(codes.InvalidArgument, clean(err, application.ErrInvalidArgument))
	case errors.Is(err, application.ErrNotFound):
		return status.Error(codes.NotFound, clean(err, application.ErrNotFound))
	case errors.Is(err, application.ErrConflict), errors.Is(err, domain.ErrInvalidTransition), errors.Is(err, domain.ErrRefundDenied):
		return status.Error(codes.FailedPrecondition, clean(err, application.ErrConflict))
	case errors.Is(err, application.ErrUnavailable):
		return status.Error(codes.Unavailable, clean(err, application.ErrUnavailable))
	default:
		return status.Error(codes.Internal, "order operation failed")
	}
}
func clean(err, marker error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), marker.Error()+":"))
}
