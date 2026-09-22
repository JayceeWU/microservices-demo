package domain

import "time"

func EnsureRoomRefundEligible(now, startsAt time.Time, adminOverride bool) error {
	if !adminOverride && !now.Before(startsAt.Add(-24*time.Hour)) {
		return ErrRefundDenied
	}
	return nil
}
