package creditclient

import (
	"context"
	"errors"
	"testing"
	"time"

	creditsv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/credits/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestBackgroundHoldCallsHaveRequestIdentity(t *testing.T) {
	ctx, cancel := callContext(context.Background(), "student", "studio", time.Second)
	defer cancel()
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok || len(md.Get("x-request-id")) != 1 || md.Get("x-request-id")[0] == "" {
		t.Fatalf("background RPC missing request identity: %v", md)
	}
	if md.Get("x-user-id")[0] != "student" || md.Get("x-studio-id")[0] != "studio" {
		t.Fatalf("background RPC missing hold owner: %v", md)
	}
}

type failingCreditRPC struct {
	creditsv1.CreditServiceClient
	err error
}

func (f failingCreditRPC) PlaceHold(context.Context, *creditsv1.PlaceHoldRequest, ...grpc.CallOption) (*creditsv1.CreditHold, error) {
	return nil, f.err
}
func (f failingCreditRPC) CaptureHold(context.Context, *creditsv1.CaptureHoldRequest, ...grpc.CallOption) (*creditsv1.CreditHold, error) {
	return nil, f.err
}
func (f failingCreditRPC) ReverseCapture(context.Context, *creditsv1.ReverseCaptureRequest, ...grpc.CallOption) (*creditsv1.CreditHold, error) {
	return nil, f.err
}

func TestCreditCommandsPreserveRecoveryErrorCategories(t *testing.T) {
	for _, code := range []codes.Code{codes.DeadlineExceeded, codes.Unavailable, codes.FailedPrecondition} {
		t.Run(code.String(), func(t *testing.T) {
			cause := status.Error(code, "credit command did not return a successful response")
			client := &Client{grpc: failingCreditRPC{err: cause}}
			_, holdErr := client.PlaceHold(context.Background(), "student", "studio", "booking", 4, time.Now(), "hold")
			for _, err := range []error{holdErr, client.Capture(context.Background(), "student", "hold", "capture", "test"), client.Reverse(context.Background(), "student", "hold", "reverse", "test")} {
				if status.Code(err) != code || !errors.Is(err, cause) {
					t.Fatalf("error category was lost: %v, want %s", err, code)
				}
			}
		})
	}
}
