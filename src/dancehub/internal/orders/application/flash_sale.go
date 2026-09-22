package application

import (
	"context"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/orders/domain"
)

func (s *Service) ReserveFlashSale(ctx context.Context, command ReserveFlashSaleCommand) (FlashSaleRequestView, error) {
	if err := requireUser(command.Actor); err != nil {
		return FlashSaleRequestView{}, err
	}
	if command.Audit.IdempotencyKey == "" {
		return FlashSaleRequestView{}, Invalid("idempotency_key is required")
	}
	campaign, err := s.Catalog.Campaign(ctx, command.Actor, command.CampaignID)
	now := s.Clock.Now()
	if err != nil || !campaign.Active || now.Before(campaign.StartsAt) || !now.Before(campaign.EndsAt) {
		return FlashSaleRequestView{}, NotFound("campaign is not active")
	}
	requestID, err := s.Admission.Reserve(ctx, command.Actor, campaign, command.Audit)
	if err != nil {
		return FlashSaleRequestView{}, err
	}
	return FlashSaleRequestView{RequestID: requestID, CampaignID: campaign.ID, Status: FlashSaleQueued}, nil
}

func (s *Service) GetFlashSaleRequest(ctx context.Context, actor ActorContext, id string) (FlashSaleRequestView, error) {
	var result FlashSaleRequestView
	err := s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.FlashSales().GetRequest(ctx, id)
		return err
	})
	return result, err
}

func (s *Service) ProcessFlashSale(ctx context.Context, message FlashSaleMessage) error {
	actor := ActorContext{ActorKind: "service", ServicePrincipal: "orderservice", RequestID: message.RequestID}
	campaign, err := s.Catalog.Campaign(ctx, actor, message.CampaignID)
	if err != nil {
		return err
	}
	product, err := s.Catalog.Product(ctx, actor, campaign.ProductVersionID)
	if err != nil {
		return err
	}
	actor.StudioID = product.StudioID
	product.FinalSale = true
	money, err := domain.NewMoney(product.AmountCents)
	if err != nil {
		return err
	}
	line := domain.CreditProductLine{ProductVersionID: product.ProductVersionID, Description: product.Name, UnitPrice: money, Count: 1, IsFinalSale: true, Scope: domain.IssuerScope(product.IssuerScope), StudioID: product.StudioID}
	aggregate, err := (domain.OrderFactory{}).Membership("", message.UserID, []domain.CreditProductLine{line}, s.Clock.Now().Add(time.Duration(campaign.PaymentTTLMinutes)*time.Minute))
	if err != nil {
		return err
	}
	return s.Work.Do(ctx, actor, "orderservice", func(ctx context.Context, repositories Repositories) error {
		_, err := repositories.FlashSales().Allocate(ctx, message, campaign, aggregate, freezeProduct(product))
		return err
	})
}
