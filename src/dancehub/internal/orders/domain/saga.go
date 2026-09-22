package domain

type PaymentEvent string

const (
	PaymentSucceededEvent PaymentEvent = "PaymentSucceeded"
	PaymentFailedEvent    PaymentEvent = "PaymentFailed"
)

type PaymentSaga struct{}

func (PaymentSaga) Apply(order *Order, event PaymentEvent) error {
	snapshot := order.Snapshot()
	switch event {
	case PaymentSucceededEvent:
		if snapshot.Status == Paid || snapshot.Status == PaidNotFulfilled || snapshot.Status == Fulfilled {
			return nil
		}
		return order.PaymentSucceeded()
	case PaymentFailedEvent:
		if snapshot.Status != PendingPayment {
			return nil
		}
		return order.PaymentFailed()
	default:
		return ErrInvalidTransition
	}
}
