package application

import (
	"context"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

type refundTestCredits struct {
	CreditPort
	calls   []string
	lines   []RefundLineRequirement
	failure error
}

func (c *refundTestCredits) ChangeRefund(_ context.Context, _, _, _ string, lines []RefundLineRequirement, operation string, _ AuditContext) error {
	c.calls = append(c.calls, operation)
	c.lines = lines
	return c.failure
}

type refundTestPayments struct {
	PaymentPort
	keys  []string
	calls int
}

func (p *refundTestPayments) GetByOrder(context.Context, ActorContext, string) (PaymentSnapshot, error) {
	return PaymentSnapshot{ID: "payment-1", AmountCents: 24000, Status: "SUCCEEDED"}, nil
}
func (p *refundTestPayments) Refund(_ context.Context, _ ActorContext, _ string, _ int64, audit AuditContext) error {
	p.calls++
	p.keys = append(p.keys, audit.IdempotencyKey)
	return nil
}

func TestDirectApprovalFreezesBeforePaymentAndIncludesQuantities(t *testing.T) {
	credits := &refundTestCredits{}
	payments := &refundTestPayments{}
	s := &Service{Credits: credits, Payments: payments}
	record := &OrderRecord{Aggregate: domain.Rehydrate(domain.Snapshot{ID: "order-1", UserID: "user-1"}), Lines: []OrderLineView{{ID: "line-1", Quantity: 2}}}
	job := &RefundAttempt{ID: "attempt-1", OrderID: "order-1", Phase: "RESERVING", Decision: "APPROVE", Kind: "CREDIT"}
	credits.failure = status.Error(codes.FailedPrecondition, "active booking")
	if _, err := s.runRefundPhase(context.Background(), ActorContext{}, job, record); err == nil || payments.calls != 0 {
		t.Fatal("ineligible credits must not cause a payment refund")
	}
	credits.failure = nil
	phase, err := s.runRefundPhase(context.Background(), ActorContext{}, job, record)
	if err != nil || phase != "REFUNDING" || payments.calls != 0 || len(credits.lines) != 1 || credits.lines[0].Quantity != 2 {
		t.Fatalf("freeze phase=%s err=%v requirements=%v", phase, err, credits.lines)
	}
	job.Phase = phase
	for i := 0; i < 2; i++ {
		next, err := s.runRefundPhase(context.Background(), ActorContext{}, job, record)
		if err != nil || next != "REVOKING" {
			t.Fatalf("payment phase=%s err=%v", next, err)
		}
	}
	if payments.keys[0] != payments.keys[1] || payments.keys[0] != "refund:order-1" {
		t.Fatal("lost responses must retry the same payment operation")
	}
	job.Phase = "REVOKING"
	credits.failure = status.Error(codes.Unavailable, "response lost")
	if _, err := s.runRefundPhase(context.Background(), ActorContext{}, job, record); err == nil {
		t.Fatal("revocation failure cannot mark a refund completed")
	}
	credits.failure = nil
	if phase, err := s.runRefundPhase(context.Background(), ActorContext{}, job, record); err != nil || phase != "COMPLETED" {
		t.Fatalf("revocation phase=%s err=%v", phase, err)
	}
}

func TestStudentRefundWaitsForDecisionAfterFreeze(t *testing.T) {
	s := &Service{Credits: &refundTestCredits{}}
	r := &OrderRecord{Aggregate: domain.Rehydrate(domain.Snapshot{ID: "o", UserID: "u"})}
	phase, err := s.runRefundPhase(context.Background(), ActorContext{}, &RefundAttempt{Phase: "RESERVING", Decision: "REQUEST"}, r)
	if err != nil || phase != "AWAITING_APPROVAL" {
		t.Fatalf("phase=%s err=%v", phase, err)
	}
}
