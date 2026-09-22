// Package domain holds catalog's value types. Catalog is a read-only
// service: studios, rooms, and credit products are administered elsewhere
// and simply displayed here, so there are no state transitions to protect
// and consequently no aggregates or invariant-bearing methods. Keeping this
// package to plain value/view types (rather than inventing lifecycle
// behavior it doesn't have) is intentional, matching ADR-001's model of
// letting the domain layer's shape follow the bounded context's actual
// complexity.
package domain

import "time"

type IssuerScope string

const (
	IssuerScopeUnspecified IssuerScope = ""
	IssuerScopePlatform    IssuerScope = "PLATFORM"
	IssuerScopeStudio      IssuerScope = "STUDIO"
)

type ProductKind string

const (
	ProductKindUnspecified ProductKind = ""
	ProductKindCredits     ProductKind = "CREDITS"
	ProductKindUnlimited   ProductKind = "UNLIMITED"
)

type Studio struct {
	ID, Slug, Name, Description                    string
	AddressLine, City, State, PostalCode, Timezone string
	ImageURL                                       string
}

type Room struct {
	ID, StudioID, Name     string
	Capacity               int32
	RentalRateCentsPerHour int64
	Facilities             []string
	ImageURL               string
}

type CreditProductVersion struct {
	ID, ProductID, StudioID    string
	IssuerScope                IssuerScope
	Kind                       ProductKind
	Name                       string
	AmountCents                int64
	CreditAmount, ValidityDays int32
	FinalSale                  bool
}

type CampaignSnapshot struct {
	ID, StudioID, ProductVersionID, Name       string
	Inventory, PerUserLimit, PaymentTTLMinutes int32
	StartsAt, EndsAt                           time.Time
	Active                                     bool
}

// ClampPageSize is the one bit of shared logic every catalog list endpoint
// needs: reject an out-of-range page size and fall back to a default rather
// than let a caller request zero or an unbounded number of rows.
func ClampPageSize(value, fallback, max int32) int32 {
	if value < 1 || value > max {
		return fallback
	}
	return value
}
