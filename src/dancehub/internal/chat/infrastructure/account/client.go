package account

import (
	"context"

	accountv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/account/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/application"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/chat/domain"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Client struct {
	client accountv1.AccountServiceClient
}

func New(client accountv1.AccountServiceClient) Client { return Client{client: client} }

func (c Client) GetPrincipal(ctx context.Context, actor application.ActorContext, userID string) (domain.Principal, error) {
	outgoing := platform.OutgoingGRPCContext(platform.HumanIdentityContext(ctx, actor.UserID, actor.StudioID, actor.TenantRoles, actor.GlobalRoles))
	response, err := c.client.GetChatPrincipal(outgoing, &accountv1.GetChatPrincipalRequest{UserId: userID})
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			return domain.Principal{}, appcore.NotFound("chat user not found")
		case codes.PermissionDenied:
			return domain.Principal{}, appcore.Denied("chat user lookup denied")
		case codes.Unavailable, codes.DeadlineExceeded:
			return domain.Principal{}, appcore.Unavailable("account service unavailable", err)
		default:
			return domain.Principal{}, err
		}
	}
	principal := domain.Principal{TeacherStudios: map[string]bool{}, AdminStudios: map[string]bool{}}
	if response.Profile != nil {
		principal.ID = response.Profile.Id
		principal.DisplayName = response.Profile.DisplayName
		principal.AvatarURL = response.Profile.AvatarUrl
	}
	for _, membership := range response.Memberships {
		if !membership.Active {
			continue
		}
		switch membership.Role {
		case accountv1.Role_ROLE_STUDENT:
			principal.Student = true
		case accountv1.Role_ROLE_TEACHER:
			principal.Teacher = true
			principal.TeacherStudios[membership.StudioId] = true
		case accountv1.Role_ROLE_STUDIO_ADMIN:
			principal.StudioAdmin = true
			principal.AdminStudios[membership.StudioId] = true
		}
	}
	return principal, nil
}

var _ application.AccountPort = Client{}
