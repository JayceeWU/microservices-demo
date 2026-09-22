package domain

import (
	"errors"
	"testing"
	"time"
)

func TestGroupCapacityAndBan(t *testing.T) {
	g, err := NewGroup(TeacherGroup, "studio", "teacher", "Dance talk", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	g.MemberCount = 999
	if err = g.Join(false); err != nil {
		t.Fatal(err)
	}
	if err = g.Join(false); !errors.Is(err, ErrGroupFull) {
		t.Fatalf("expected full, got %v", err)
	}
	if err = g.Join(true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected banned, got %v", err)
	}
}

func TestMessageWindows(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	m, err := NewMessage("conversation", "", "student", "client", TextMessage, "hello", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Edit("student", "updated", now.Add(14*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = m.Edit("student", "late", now.Add(16*time.Minute)); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("expected edit deadline, got %v", err)
	}
	if err = m.Withdraw("teacher", true, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestDirectRequiresStudentAndTeacher(t *testing.T) {
	if !CanStartDirect(Principal{ID: "student", Student: true}, Principal{ID: "teacher", Teacher: true}) {
		t.Fatal("student and teacher should be allowed")
	}
	if CanStartDirect(Principal{ID: "teacher", Teacher: true}, Principal{ID: "admin", StudioAdmin: true}) {
		t.Fatal("teacher/admin direct should be rejected")
	}
	if CanStartDirect(Principal{ID: "same", Student: true}, Principal{ID: "same", Teacher: true}) {
		t.Fatal("a multi-role account cannot open a direct conversation with itself")
	}
}

func TestGroupMembershipHistoryAndBan(t *testing.T) {
	membership := GroupMembership{ConversationID: "group", UserID: "student", State: "LEFT"}
	if err := membership.Join(42, false); err != nil {
		t.Fatal(err)
	}
	if membership.VisibleFromSequence != 43 || membership.State != "ACTIVE" {
		t.Fatalf("unexpected join state: %+v", membership)
	}
	if err := membership.Ban(true); err != nil {
		t.Fatal(err)
	}
	if err := membership.Join(50, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("banned member rejoined: %v", err)
	}
	if err := membership.Unban(true); err != nil {
		t.Fatal(err)
	}
	if err := membership.Join(50, false); err != nil || membership.VisibleFromSequence != 51 {
		t.Fatalf("unbanned member could not rejoin: %+v %v", membership, err)
	}
}

func TestMessageUnicodeLimitAndBlockRelation(t *testing.T) {
	now := time.Now()
	if _, err := NewMessage("conversation", "", "student", "client", TextMessage, string(make([]rune, 4001)), "", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected unicode length validation, got %v", err)
	}
	if _, err := NewBlockRelation("student", "student"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("self block must fail, got %v", err)
	}
}
