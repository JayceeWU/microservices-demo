package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type StudioID string
type UserID string
type ClassSessionID string
type BookingID string
type RoomReservationID string

func validateID(name, value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%w: %s must be a UUID", ErrInvalidArgument, name)
	}
	return nil
}

func NewStudioID(value string) (StudioID, error) {
	if err := validateID("studio id", value); err != nil {
		return "", err
	}
	return StudioID(value), nil
}

func NewUserID(value string) (UserID, error) {
	if err := validateID("user id", value); err != nil {
		return "", err
	}
	return UserID(value), nil
}

func NewClassSessionID(value string) (ClassSessionID, error) {
	if err := validateID("class session id", value); err != nil {
		return "", err
	}
	return ClassSessionID(value), nil
}

func NewBookingID(value string) (BookingID, error) {
	if err := validateID("booking id", value); err != nil {
		return "", err
	}
	return BookingID(value), nil
}

func NewRoomReservationID(value string) (RoomReservationID, error) {
	if err := validateID("room reservation id", value); err != nil {
		return "", err
	}
	return RoomReservationID(value), nil
}

type ClassTimeRange struct {
	startsAt time.Time
	endsAt   time.Time
}

func NewClassTimeRange(startsAt, endsAt, now time.Time) (ClassTimeRange, error) {
	result, err := RehydrateClassTimeRange(startsAt, endsAt)
	if err != nil {
		return ClassTimeRange{}, err
	}
	if !startsAt.After(now) {
		return ClassTimeRange{}, fmt.Errorf("%w: class must start in the future", ErrInvalidArgument)
	}
	return result, nil
}

func RehydrateClassTimeRange(startsAt, endsAt time.Time) (ClassTimeRange, error) {
	duration := endsAt.Sub(startsAt)
	if duration <= 0 || duration%(15*time.Minute) != 0 {
		return ClassTimeRange{}, fmt.Errorf("%w: class duration must use positive 15-minute units", ErrInvalidArgument)
	}
	return ClassTimeRange{startsAt: startsAt.UTC(), endsAt: endsAt.UTC()}, nil
}

func (r ClassTimeRange) StartsAt() time.Time     { return r.startsAt }
func (r ClassTimeRange) EndsAt() time.Time       { return r.endsAt }
func (r ClassTimeRange) Duration() time.Duration { return r.endsAt.Sub(r.startsAt) }
func (r ClassTimeRange) CreditCost() CreditCost  { return CreditCost(r.Duration() / (15 * time.Minute)) }

type Capacity int32

func NewCapacity(value int32) (Capacity, error) {
	if value < 1 {
		return 0, fmt.Errorf("%w: capacity must be positive", ErrInvalidArgument)
	}
	return Capacity(value), nil
}

type MinimumStudents int32

func NewMinimumStudents(value int32, capacity Capacity) (MinimumStudents, error) {
	if value < 1 || value > int32(capacity) {
		return 0, fmt.Errorf("%w: minimum students must be between one and capacity", ErrInvalidArgument)
	}
	return MinimumStudents(value), nil
}

type CreditCost int32

func NewCreditCost(value int32) (CreditCost, error) {
	if value < 1 {
		return 0, fmt.Errorf("%w: credit cost must be positive", ErrInvalidArgument)
	}
	return CreditCost(value), nil
}
