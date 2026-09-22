package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestMoneyAndFactory(t *testing.T) {
	price, _ := NewMoney(1500)
	order, err := (OrderFactory{}).Membership("o", "u", []CreditProductLine{{ProductVersionID: "p", UnitPrice: price, Count: 2, Scope: StudioScope, StudioID: "s"}}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if order.Snapshot().Total.Cents() != 3000 {
		t.Fatal("wrong total")
	}
	large, _ := NewMoney(math.MaxInt64)
	if _, err = large.Multiply(2); !errors.Is(err, ErrInvalidMoney) {
		t.Fatal("overflow expected")
	}
}
func TestPaymentSagaIsIdempotent(t *testing.T) {
	price, _ := NewMoney(100)
	order, _ := (OrderFactory{}).Membership("o", "u", []CreditProductLine{{ProductVersionID: "p", UnitPrice: price, Count: 1, Scope: PlatformScope}}, time.Now().Add(time.Hour))
	saga := PaymentSaga{}
	if err := saga.Apply(order, PaymentSucceededEvent); err != nil {
		t.Fatal(err)
	}
	if err := saga.Apply(order, PaymentSucceededEvent); err != nil {
		t.Fatal(err)
	}
	if order.Snapshot().Status != Paid {
		t.Fatal("payment did not advance order")
	}
}
func TestFactoryRejectsMixedIssuerScopes(t *testing.T) {
	price, _ := NewMoney(100)
	_, err := (OrderFactory{}).Membership("o", "u", []CreditProductLine{
		{ProductVersionID: "studio", UnitPrice: price, Count: 1, Scope: StudioScope, StudioID: "s"},
		{ProductVersionID: "platform", UnitPrice: price, Count: 1, Scope: PlatformScope},
	}, time.Now().Add(time.Hour))
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("mixed issuer order should fail, got %v", err)
	}
}

func TestRoomRefundPolicyClosesAtTwentyFourHours(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	starts := now.Add(24 * time.Hour)
	if err := EnsureRoomRefundEligible(now, starts, false); !errors.Is(err, ErrRefundDenied) {
		t.Fatalf("refund should be denied at T-24, got %v", err)
	}
	if err := EnsureRoomRefundEligible(now, starts, true); err != nil {
		t.Fatalf("admin override failed: %v", err)
	}
}

func TestIllegalOrderTransitionIsRejected(t *testing.T) {
	price, _ := NewMoney(100)
	order, _ := (OrderFactory{}).Membership("o", "u", []CreditProductLine{{ProductVersionID: "p", UnitPrice: price, Count: 1, Scope: PlatformScope}}, time.Now().Add(time.Hour))
	if err := order.Fulfill(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("fulfillment before payment should fail, got %v", err)
	}
}

func TestPaymentRetrySuccessAndStaleFailure(t *testing.T) {
	order := Rehydrate(Snapshot{Status: PaymentFailed})
	if err := (PaymentSaga{}).Apply(order, PaymentSucceededEvent); err != nil {
		t.Fatal(err)
	}
	if order.Snapshot().Status != Paid {
		t.Fatal("successful retry did not recover failed order")
	}
	if err := (PaymentSaga{}).Apply(order, PaymentFailedEvent); err != nil || order.Snapshot().Status != Paid {
		t.Fatal("late failure regressed successful payment")
	}
}
