package application

import (
	"context"
	"strings"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/domain"
)

const serviceName = "catalogservice"

// productSnapshotCurrency matches the constant "USD" the flat handler used
// for every GetProductSnapshot response; catalog does not yet model
// multi-currency products.
const productSnapshotCurrency = "USD"

type Service struct {
	Work UnitOfWork
}

func NewService(work UnitOfWork) *Service {
	return &Service{Work: work}
}

type SearchStudiosQuery struct {
	Query string
	Page  PageRequest
}

func (s *Service) SearchStudios(ctx context.Context, actor ActorContext, query SearchStudiosQuery) ([]domain.Studio, error) {
	likeQuery := "%" + strings.TrimSpace(query.Query) + "%"
	limit := domain.ClampPageSize(query.Page.Size, 24, 100)
	var result []domain.Studio
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Studios().Search(ctx, likeQuery, limit)
		return err
	})
	return result, err
}

func (s *Service) GetStudio(ctx context.Context, actor ActorContext, id string) (domain.Studio, error) {
	var result domain.Studio
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Studios().Get(ctx, id)
		return err
	})
	return result, err
}

func (s *Service) BatchGetStudios(ctx context.Context, actor ActorContext, ids []string) ([]domain.Studio, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result []domain.Studio
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Studios().BatchGet(ctx, ids)
		return err
	})
	return result, err
}

type ListRoomsQuery struct {
	StudioID string
	Page     PageRequest
}

func (s *Service) ListRooms(ctx context.Context, actor ActorContext, query ListRoomsQuery) ([]domain.Room, error) {
	limit := domain.ClampPageSize(query.Page.Size, 24, 100)
	var result []domain.Room
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Rooms().ListByStudio(ctx, query.StudioID, limit)
		return err
	})
	return result, err
}

func (s *Service) GetRoom(ctx context.Context, actor ActorContext, id, studioID string) (domain.Room, error) {
	var result domain.Room
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Rooms().Get(ctx, id, studioID)
		return err
	})
	return result, err
}

type SearchCreditProductsQuery struct {
	StudioID        string
	IncludePlatform bool
	Page            PageRequest
}

func (s *Service) SearchCreditProducts(ctx context.Context, actor ActorContext, query SearchCreditProductsQuery) ([]domain.CreditProductVersion, error) {
	limit := domain.ClampPageSize(query.Page.Size, 24, 100)
	var result []domain.CreditProductVersion
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Products().Search(ctx, query.StudioID, query.IncludePlatform, limit)
		return err
	})
	return result, err
}

func (s *Service) GetCreditProductVersion(ctx context.Context, actor ActorContext, id string) (domain.CreditProductVersion, error) {
	var result domain.CreditProductVersion
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Products().Get(ctx, id)
		return err
	})
	return result, err
}

func (s *Service) GetProductSnapshot(ctx context.Context, actor ActorContext, productVersionID string) (ProductSnapshotView, error) {
	version, err := s.GetCreditProductVersion(ctx, actor, productVersionID)
	if err != nil {
		return ProductSnapshotView{}, err
	}
	return ProductSnapshotView{Version: version, Currency: productSnapshotCurrency}, nil
}

func (s *Service) GetCampaign(ctx context.Context, actor ActorContext, id string) (domain.CampaignSnapshot, error) {
	var result domain.CampaignSnapshot
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Campaigns().Get(ctx, id)
		return err
	})
	return result, err
}
