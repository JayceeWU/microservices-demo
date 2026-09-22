package creditclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	creditsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/credits/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Client struct {
	grpc creditsv1.CreditServiceClient
	conn *grpc.ClientConn
}

type Hold struct {
	ID        string
	BookingID string
	Amount    int
	Status    string
}

func New(address string) *Client {
	conn, err := platform.DialGRPC(address)
	if err != nil {
		panic(fmt.Sprintf("credit gRPC configuration failed: %v", err))
	}
	return &Client{grpc: creditsv1.NewCreditServiceClient(conn), conn: conn}
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) CompensateBooking(ctx context.Context, userID, studioID, bookingID, key, reason string) error {
	ctx = withWorkerRequestID(ctx)
	ctx = platform.ServiceIdentityContext(ctx, "scheduling-worker")
	call, cancel := context.WithTimeout(platform.OutgoingGRPCContext(ctx), 10*time.Second)
	defer cancel()
	_, err := c.grpc.CompensateBookingHold(call, &creditsv1.CompensateBookingHoldRequest{BookingId: bookingID, UserId: userID, StudioId: studioID, Audit: audit(key, reason)})
	return err
}

func (c *Client) PlaceHold(ctx context.Context, userID, studioID, bookingID string, amount int, classStartsAt time.Time, key string) (Hold, error) {
	callContext, cancel := callContext(ctx, userID, studioID, 10*time.Second)
	defer cancel()
	value, err := c.grpc.PlaceHold(callContext, &creditsv1.PlaceHoldRequest{StudioId: studioID, BookingId: bookingID, Amount: &commonv1.CreditAmount{Units: int32(amount)}, ClassStartsAt: timestamppb.New(classStartsAt), Audit: &commonv1.AuditContext{IdempotencyKey: key}})
	if err != nil {
		return Hold{}, fmt.Errorf("credit hold failed: %w", err)
	}
	return Hold{ID: value.Id, BookingID: value.BookingId, Amount: int(value.GetAmount().GetUnits()), Status: strings.TrimPrefix(value.Status.String(), "CREDIT_HOLD_STATUS_")}, nil
}

func (c *Client) Capture(ctx context.Context, userID, holdID, key, reason string) error {
	callContext, cancel := callContext(ctx, userID, "", 10*time.Second)
	defer cancel()
	_, err := c.grpc.CaptureHold(callContext, &creditsv1.CaptureHoldRequest{HoldId: holdID, Audit: audit(key, reason)})
	return grpcError(err)
}

func (c *Client) Release(ctx context.Context, userID, holdID, key, reason string) error {
	callContext, cancel := callContext(ctx, userID, "", 10*time.Second)
	defer cancel()
	_, err := c.grpc.ReleaseHold(callContext, &creditsv1.ReleaseHoldRequest{HoldId: holdID, Audit: audit(key, reason)})
	return grpcError(err)
}

func (c *Client) Reverse(ctx context.Context, userID, holdID, key, reason string) error {
	callContext, cancel := callContext(ctx, userID, "", 10*time.Second)
	defer cancel()
	_, err := c.grpc.ReverseCapture(callContext, &creditsv1.ReverseCaptureRequest{HoldId: holdID, Audit: audit(key, reason)})
	return grpcError(err)
}

func callContext(ctx context.Context, userID, studioID string, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = withWorkerRequestID(ctx)
	tenantRoles := platform.TenantRoles(ctx)
	globalRoles := platform.GlobalRoles(ctx)
	if studioID == "" {
		studioID, _ = platform.StudioID(ctx)
	}
	ctx = platform.HumanIdentityContext(ctx, userID, studioID, tenantRoles, globalRoles)
	return context.WithTimeout(platform.OutgoingGRPCContext(ctx), timeout)
}

func withWorkerRequestID(ctx context.Context) context.Context {
	identity := platform.Actor(ctx)
	if identity.RequestID == "" {
		identity.RequestID = "scheduling-worker"
		ctx = platform.WithActor(ctx, identity)
	}
	return ctx
}

func audit(key, reason string) *commonv1.AuditContext {
	return &commonv1.AuditContext{IdempotencyKey: key, Reason: reason}
}

func grpcError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("credit command failed: %w", err)
}
