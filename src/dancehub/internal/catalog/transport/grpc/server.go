package grpctransport

import (
	"context"
	"errors"
	"strings"

	catalogv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/catalog/v1"
	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/catalog/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	catalogv1.UnimplementedCatalogServiceServer
	service catalogApplication
}

type catalogApplication interface {
	SearchStudios(context.Context, application.ActorContext, application.SearchStudiosQuery) ([]domain.Studio, error)
	GetStudio(context.Context, application.ActorContext, string) (domain.Studio, error)
	BatchGetStudios(context.Context, application.ActorContext, []string) ([]domain.Studio, error)
	ListRooms(context.Context, application.ActorContext, application.ListRoomsQuery) ([]domain.Room, error)
	GetRoom(context.Context, application.ActorContext, string, string) (domain.Room, error)
	SearchCreditProducts(context.Context, application.ActorContext, application.SearchCreditProductsQuery) ([]domain.CreditProductVersion, error)
	GetCreditProductVersion(context.Context, application.ActorContext, string) (domain.CreditProductVersion, error)
	GetProductSnapshot(context.Context, application.ActorContext, string) (application.ProductSnapshotView, error)
	GetCampaign(context.Context, application.ActorContext, string) (domain.CampaignSnapshot, error)
}

func NewServer(service catalogApplication) *Server { return &Server{service: service} }

func (s *Server) SearchStudios(ctx context.Context, request *catalogv1.SearchStudiosRequest) (*catalogv1.SearchStudiosResponse, error) {
	items, err := s.service.SearchStudios(ctx, actor(ctx), application.SearchStudiosQuery{Query: request.Query, Page: page(request.Page)})
	if err != nil {
		return nil, grpcError(err)
	}
	response := &catalogv1.SearchStudiosResponse{Page: &commonv1.PageResponse{}}
	for _, item := range items {
		response.Studios = append(response.Studios, studio(item))
	}
	return response, nil
}

func (s *Server) GetStudio(ctx context.Context, request *catalogv1.GetStudioRequest) (*catalogv1.Studio, error) {
	item, err := s.service.GetStudio(ctx, actor(ctx), request.Id)
	if err != nil {
		return nil, grpcError(err)
	}
	return studio(item), nil
}

func (s *Server) BatchGetStudios(ctx context.Context, request *catalogv1.BatchGetStudiosRequest) (*catalogv1.BatchGetStudiosResponse, error) {
	items, err := s.service.BatchGetStudios(ctx, actor(ctx), request.Ids)
	if err != nil {
		return nil, grpcError(err)
	}
	response := &catalogv1.BatchGetStudiosResponse{}
	for _, item := range items {
		response.Studios = append(response.Studios, studio(item))
	}
	return response, nil
}

func (s *Server) ListRooms(ctx context.Context, request *catalogv1.ListRoomsRequest) (*catalogv1.ListRoomsResponse, error) {
	items, err := s.service.ListRooms(ctx, actor(ctx), application.ListRoomsQuery{StudioID: request.StudioId, Page: page(request.Page)})
	if err != nil {
		return nil, grpcError(err)
	}
	response := &catalogv1.ListRoomsResponse{Page: &commonv1.PageResponse{}}
	for _, item := range items {
		response.Rooms = append(response.Rooms, room(item))
	}
	return response, nil
}

func (s *Server) GetRoom(ctx context.Context, request *catalogv1.GetRoomRequest) (*catalogv1.Room, error) {
	item, err := s.service.GetRoom(ctx, actor(ctx), request.Id, request.StudioId)
	if err != nil {
		return nil, grpcError(err)
	}
	return room(item), nil
}

func (s *Server) SearchCreditProducts(ctx context.Context, request *catalogv1.SearchCreditProductsRequest) (*catalogv1.SearchCreditProductsResponse, error) {
	items, err := s.service.SearchCreditProducts(ctx, actor(ctx), application.SearchCreditProductsQuery{StudioID: request.StudioId, IncludePlatform: request.IncludePlatform, Page: page(request.Page)})
	if err != nil {
		return nil, grpcError(err)
	}
	response := &catalogv1.SearchCreditProductsResponse{Page: &commonv1.PageResponse{}}
	for _, item := range items {
		response.Products = append(response.Products, product(item))
	}
	return response, nil
}

func (s *Server) GetCreditProductVersion(ctx context.Context, request *catalogv1.GetCreditProductVersionRequest) (*catalogv1.CreditProductVersion, error) {
	item, err := s.service.GetCreditProductVersion(ctx, actor(ctx), request.Id)
	if err != nil {
		return nil, grpcError(err)
	}
	return product(item), nil
}

func (s *Server) GetProductSnapshot(ctx context.Context, request *catalogv1.GetProductSnapshotRequest) (*catalogv1.ProductSnapshot, error) {
	item, err := s.service.GetProductSnapshot(ctx, actor(ctx), request.ProductVersionId)
	if err != nil {
		return nil, grpcError(err)
	}
	return &catalogv1.ProductSnapshot{Version: product(item.Version), Currency: item.Currency}, nil
}

func (s *Server) GetCampaign(ctx context.Context, request *catalogv1.GetCampaignRequest) (*catalogv1.CampaignSnapshot, error) {
	item, err := s.service.GetCampaign(ctx, actor(ctx), request.Id)
	if err != nil {
		return nil, grpcError(err)
	}
	return campaign(item), nil
}

func actor(ctx context.Context) application.ActorContext {
	return platform.Actor(ctx)
}

func page(value *commonv1.PageRequest) application.PageRequest {
	if value == nil {
		return application.PageRequest{}
	}
	return application.PageRequest{Size: value.PageSize, Token: value.PageToken}
}

func studio(value domain.Studio) *catalogv1.Studio {
	return &catalogv1.Studio{
		Id: value.ID, Slug: value.Slug, Name: value.Name, Description: value.Description,
		AddressLine: value.AddressLine, City: value.City, State: value.State, PostalCode: value.PostalCode,
		Timezone: value.Timezone, ImageUrl: value.ImageURL,
	}
}

func room(value domain.Room) *catalogv1.Room {
	return &catalogv1.Room{
		Id: value.ID, StudioId: value.StudioID, Name: value.Name, Capacity: value.Capacity,
		RentalRateCentsPerHour: value.RentalRateCentsPerHour, Facilities: value.Facilities, ImageUrl: value.ImageURL,
	}
}

func product(value domain.CreditProductVersion) *catalogv1.CreditProductVersion {
	return &catalogv1.CreditProductVersion{
		Id: value.ID, ProductId: value.ProductID, StudioId: value.StudioID, IssuerScope: issuerScope(value.IssuerScope),
		Kind: productKind(value.Kind), Name: value.Name, AmountCents: value.AmountCents,
		CreditAmount: value.CreditAmount, ValidityDays: value.ValidityDays, FinalSale: value.FinalSale,
	}
}

func campaign(value domain.CampaignSnapshot) *catalogv1.CampaignSnapshot {
	return &catalogv1.CampaignSnapshot{
		Id: value.ID, StudioId: value.StudioID, ProductVersionId: value.ProductVersionID, Inventory: value.Inventory,
		PerUserLimit: value.PerUserLimit, PaymentTtlMinutes: value.PaymentTTLMinutes, Name: value.Name,
		StartsAt: timestamppb.New(value.StartsAt), EndsAt: timestamppb.New(value.EndsAt), Active: value.Active,
	}
}

func issuerScope(value domain.IssuerScope) catalogv1.IssuerScope {
	if value == domain.IssuerScopePlatform {
		return catalogv1.IssuerScope_ISSUER_SCOPE_PLATFORM
	}
	return catalogv1.IssuerScope_ISSUER_SCOPE_STUDIO
}

func productKind(value domain.ProductKind) catalogv1.ProductKind {
	if value == domain.ProductKindUnlimited {
		return catalogv1.ProductKind_PRODUCT_KIND_UNLIMITED
	}
	return catalogv1.ProductKind_PRODUCT_KIND_CREDITS
}

func grpcError(err error) error {
	switch {
	case errors.Is(err, application.ErrNotFound):
		return status.Error(codes.NotFound, clean(err, application.ErrNotFound))
	case errors.Is(err, application.ErrConflict):
		return status.Error(codes.FailedPrecondition, clean(err, application.ErrConflict))
	default:
		return status.Error(codes.Internal, "catalog operation failed")
	}
}

func clean(err error, marker error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), marker.Error()+":"))
}
