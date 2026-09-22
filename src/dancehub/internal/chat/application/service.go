package application

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
)

type Service struct {
	uow      UnitOfWork
	accounts AccountPort
	catalog  CatalogPort
	objects  ObjectStore
	media    MediaQueue
	tickets  TicketStore
	bus      RealtimeBus
	clock    Clock
	ids      IDGenerator
}

func NewService(uow UnitOfWork, accounts AccountPort, catalog CatalogPort, objects ObjectStore, media MediaQueue, tickets TicketStore, bus RealtimeBus, clock Clock, ids IDGenerator) *Service {
	return &Service{uow: uow, accounts: accounts, catalog: catalog, objects: objects, media: media, tickets: tickets, bus: bus, clock: clock, ids: ids}
}

func requireActor(actor ActorContext) error {
	if actor.UserID == "" {
		return appcore.ErrUnauthenticated
	}
	return nil
}

func pageOrDefault(page Page) Page {
	if page.Size <= 0 || page.Size > 100 {
		page.Size = 50
	}
	return page
}

func translateDomain(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrBlocked):
		return appcore.Denied(err.Error())
	case errors.Is(err, domain.ErrInvalid):
		return appcore.Invalid(err.Error())
	case errors.Is(err, domain.ErrGroupFull), errors.Is(err, domain.ErrWindowClosed):
		return appcore.Conflict(err.Error())
	default:
		return err
	}
}

func (s *Service) ListConversations(ctx context.Context, actor ActorContext, page Page) ([]ConversationView, string, error) {
	if err := requireActor(actor); err != nil {
		return nil, "", err
	}
	var result []ConversationView
	var next string
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var err error
		result, next, err = repos.Conversations().List(ctx, actor.UserID, pageOrDefault(page))
		return err
	})
	return result, next, err
}

func (s *Service) ListGroups(ctx context.Context, actor ActorContext, studioID, teacherID string, page Page) ([]ConversationView, string, error) {
	// Group metadata is public. The empty actor intentionally gets the guest role for RLS.
	if actor.UserID == "" {
		actor.TenantRoles = []string{"guest"}
		actor.ActorKind = "human"
	}
	var result []ConversationView
	var next string
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var err error
		result, next, err = repos.Conversations().ListGroups(ctx, actor.UserID, studioID, teacherID, pageOrDefault(page))
		return err
	})
	return result, next, err
}

func (s *Service) CreateTicket(ctx context.Context, actor ActorContext) (string, time.Time, error) {
	if err := requireActor(actor); err != nil {
		return "", time.Time{}, err
	}
	expires := s.clock.Now().Add(30 * time.Second)
	ticket, err := s.tickets.Create(ctx, actor, 30*time.Second)
	return ticket, expires, err
}

func (s *Service) ConsumeTicket(ctx context.Context, ticket string) (ActorContext, error) {
	return s.tickets.Consume(ctx, ticket)
}
func (s *Service) Bus() RealtimeBus { return s.bus }

// PublishPresence fans presence metadata only to conversations the actor can
// currently access. The Redis key remains the source of truth and naturally
// expires when heartbeats stop.
func (s *Service) PublishPresence(ctx context.Context, actor ActorContext, online bool) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	page := Page{Size: 100}
	for {
		items, next, err := s.ListConversations(ctx, actor, page)
		if err != nil {
			return err
		}
		for _, item := range items {
			payload, marshalErr := json.Marshal(map[string]any{
				"event_type": "presence", "conversation_id": item.Conversation.ID,
				"user_id": actor.UserID, "online": online,
			})
			if marshalErr != nil {
				return marshalErr
			}
			if err = s.bus.Publish(ctx, payload); err != nil {
				return err
			}
		}
		if next == "" {
			return nil
		}
		page.Token = next
	}
}

func (s *Service) AuthorizeConversation(ctx context.Context, actor ActorContext, conversationID string) (bool, error) {
	var allowed bool
	err := s.uow.Do(ctx, actor, func(ctx context.Context, repos Repositories) error {
		var err error
		allowed, _, err = repos.Conversations().CanAccess(ctx, conversationID, actor.UserID)
		return err
	})
	return allowed, err
}

func SortConversationViews(items []ConversationView) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Conversation.CreatedAt.After(items[j].Conversation.CreatedAt) })
}
