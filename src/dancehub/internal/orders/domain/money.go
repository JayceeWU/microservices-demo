package domain

import (
	"fmt"
	"math"
)

type Money struct{ cents int64 }

func NewMoney(cents int64) (Money, error) {
	if cents < 0 {
		return Money{}, fmt.Errorf("%w: cents must be non-negative", ErrInvalidMoney)
	}
	return Money{cents: cents}, nil
}
func (m Money) Cents() int64 { return m.cents }
func (m Money) Add(other Money) (Money, error) {
	if other.cents > math.MaxInt64-m.cents {
		return Money{}, fmt.Errorf("%w: overflow", ErrInvalidMoney)
	}
	return Money{cents: m.cents + other.cents}, nil
}
func (m Money) Multiply(quantity int32) (Money, error) {
	if quantity < 1 {
		return Money{}, fmt.Errorf("%w: quantity must be positive", ErrInvalidMoney)
	}
	if m.cents > math.MaxInt64/int64(quantity) {
		return Money{}, fmt.Errorf("%w: overflow", ErrInvalidMoney)
	}
	return Money{cents: m.cents * int64(quantity)}, nil
}
