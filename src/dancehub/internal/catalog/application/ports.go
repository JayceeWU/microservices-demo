package application

import (
	"context"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/domain"
)

var (
	ErrNotFound = appcore.ErrNotFound
	ErrConflict = appcore.ErrConflict
	NotFound    = appcore.NotFound
	Conflict    = appcore.Conflict
)

type ActorContext = appcore.ActorContext
type PageRequest = appcore.PageRequest

// UnitOfWork creates transaction-scoped repositories after applying the RLS
// identity with SET LOCAL, matching every other layered service even though
// catalog's transactions are all read-only.
type UnitOfWork interface {
	Do(context.Context, ActorContext, string, func(context.Context, Repositories) error) error
}

type Repositories interface {
	Studios() StudioRepository
	Rooms() RoomRepository
	Products() ProductRepository
	Campaigns() CampaignRepository
}

type ProductSnapshotView struct {
	Version  domain.CreditProductVersion
	Currency string
}

type StudioRepository interface {
	Search(ctx context.Context, query string, limit int32) ([]domain.Studio, error)
	Get(ctx context.Context, id string) (domain.Studio, error)
	BatchGet(ctx context.Context, ids []string) ([]domain.Studio, error)
}

type RoomRepository interface {
	ListByStudio(ctx context.Context, studioID string, limit int32) ([]domain.Room, error)
	Get(ctx context.Context, id, studioID string) (domain.Room, error)
}

type ProductRepository interface {
	Search(ctx context.Context, studioID string, includePlatform bool, limit int32) ([]domain.CreditProductVersion, error)
	Get(ctx context.Context, id string) (domain.CreditProductVersion, error)
}

type CampaignRepository interface {
	Get(ctx context.Context, id string) (domain.CampaignSnapshot, error)
}
