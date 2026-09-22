package main

import (
	"context"
	"testing"

	payrollv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payroll/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCalculatePayUsesFrozenDurationAndEffectiveRedemptions(t *testing.T) {
	base, attendance, total := calculatePay(75, 12)
	if base != 3750 || attendance != 4800 || total != 8550 {
		t.Fatalf("unexpected payroll: base=%d attendance=%d total=%d", base, attendance, total)
	}
}

func TestCalculatePaySupportsFifteenMinuteUnits(t *testing.T) {
	base, attendance, total := calculatePay(15, 0)
	if base != 750 || attendance != 0 || total != 750 {
		t.Fatalf("unexpected 15-minute payroll: %d %d %d", base, attendance, total)
	}
}

func TestPayrollRejectsUnauthorizedReadsBeforeDatabaseAccess(t *testing.T) {
	const teacher = "30000000-0000-0000-0000-000000000002"
	const studio = "10000000-0000-0000-0000-000000000001"
	const otherStudio = "10000000-0000-0000-0000-000000000002"
	for _, tc := range []struct {
		name, kind, selectedStudio, role, requestedStudio, requestedTeacher, month string
		want                                                                       codes.Code
	}{
		{"student", "human", studio, "student", studio, "", "2026-09", codes.PermissionDenied},
		{"another teacher", "human", studio, "teacher", studio, "30000000-0000-0000-0000-000000000003", "2026-09", codes.PermissionDenied},
		{"teacher cross studio", "human", studio, "teacher", otherStudio, "", "2026-09", codes.PermissionDenied},
		{"administrator cross studio", "human", studio, "studio_admin", otherStudio, "", "2026-09", codes.PermissionDenied},
		{"no selected studio", "human", "", "teacher", studio, "", "2026-09", codes.PermissionDenied},
		{"service with human metadata", "service", studio, "studio_admin", studio, "", "2026-09", codes.Unauthenticated},
		{"invalid month", "human", studio, "teacher", studio, "", "2026-13", codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := platform.WithActor(context.Background(), appcore.NewActorContext(teacher, tc.selectedStudio, "test", []string{tc.role}, nil, tc.kind, "test-service"))
			// A nil database deliberately proves denial happens before data access.
			_, err := (&payrollServer{}).GetMonthlyPayroll(ctx, &payrollv1.GetMonthlyPayrollRequest{StudioId: tc.requestedStudio, TeacherId: tc.requestedTeacher, Month: tc.month})
			if status.Code(err) != tc.want {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	_, err := (&payrollServer{}).GetMonthlyPayroll(context.Background(), &payrollv1.GetMonthlyPayrollRequest{StudioId: studio, Month: "2026-09"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous payroll read was not rejected: %v", err)
	}
}
