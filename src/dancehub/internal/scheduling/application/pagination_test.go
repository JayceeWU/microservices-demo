package application

import (
	"context"
	"errors"
	"testing"
)

type querySessions struct {
	ClassSessionRepository
	query SearchSessionsQuery
}

func (r *querySessions) Search(_ context.Context, query SearchSessionsQuery) (PageResult[SessionView], error) {
	r.query = query
	return PageResult[SessionView]{NextPageToken: "next"}, nil
}

type queryRepositories struct {
	Repositories
	sessions *querySessions
}

func (r queryRepositories) Sessions() ClassSessionRepository { return r.sessions }

func TestSessionQueryBindsCursorScopeToTrustedActor(t *testing.T) {
	sessions := &querySessions{}
	service := Service{Work: &fakeWork{repositories: queryRepositories{sessions: sessions}}}
	actor := ActorContext{UserID: testUser, StudioID: testStudio}
	result, err := service.SearchSessions(context.Background(), actor, SearchSessionsQuery{
		StudioID: testStudio, ViewerID: "supplied-user", TenantID: "supplied-tenant", Page: PageRequest{Size: 24, Token: "previous"},
	})
	if err != nil || result.NextPageToken != "next" {
		t.Fatalf("lost query result: %+v %v", result, err)
	}
	if sessions.query.ViewerID != actor.UserID || sessions.query.TenantID != actor.StudioID {
		t.Fatal("cursor scope used caller-supplied identity")
	}
	if sessions.query.Page.Token != "previous" || sessions.query.Page.Size != 24 {
		t.Fatal("lost page request")
	}
	for _, query := range []SearchSessionsQuery{{StudioID: "invalid"}, {TeacherID: "invalid"}} {
		if _, err := service.SearchSessions(context.Background(), actor, query); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid UUID filter was accepted: %v", err)
		}
	}
}
