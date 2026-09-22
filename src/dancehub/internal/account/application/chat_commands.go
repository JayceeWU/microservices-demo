package application

import (
	"context"
	"strings"
)

func (s *Service) GetChatPrincipal(ctx context.Context, actor ActorContext, userID string) (ChatPrincipalView, error) {
	if strings.TrimSpace(userID) == "" {
		return ChatPrincipalView{}, Invalid("user_id is required")
	}
	var result ChatPrincipalView
	err := s.Work.Do(ctx, actor, serviceName, func(ctx context.Context, repositories Repositories) error {
		var err error
		result, err = repositories.Chat().GetPrincipal(ctx, userID)
		return err
	})
	return result, err
}
