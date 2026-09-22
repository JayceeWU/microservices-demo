package main

import (
	"context"
	"log"
	"strings"
	"time"

	commonv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/common/v1"
	payrollv1 "github.com/JayceeWU/microservices-demo/dancehub/gen/payroll/v1"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/payroll/projection"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type payrollServer struct {
	payrollv1.UnimplementedPayrollServiceServer
	db *pgxpool.Pool
}

func main() {
	db := platform.OpenDatabase()
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumer := &projection.Consumer{Brokers: strings.Split(platform.MustEnv("KAFKA_BOOTSTRAP_SERVERS"), ","), Store: projection.PostgresStore{Pool: db}}
	go consumer.Run(ctx)
	server := &payrollServer{db: db}
	log.Fatal(platform.ServeGRPCAndHealth("payrollservice", func(grpcServer *grpc.Server) {
		payrollv1.RegisterPayrollServiceServer(grpcServer, server)
	}, func(ctx context.Context) error {
		if err := db.Ping(ctx); err != nil {
			return err
		}
		return consumer.Health(ctx)
	}))
}

func (s *payrollServer) GetMonthlyPayroll(ctx context.Context, request *payrollv1.GetMonthlyPayrollRequest) (*payrollv1.GetMonthlyPayrollResponse, error) {
	actorID, authErr := platform.UserID(ctx)
	if authErr != nil || platform.ActorKind(ctx) != "human" {
		return nil, status.Error(codes.Unauthenticated, "authenticated human user is required")
	}
	studioID := strings.TrimSpace(request.GetStudioId())
	monthText := strings.TrimSpace(request.GetMonth())
	teacherID := strings.TrimSpace(request.GetTeacherId())
	if studioID == "" || monthText == "" {
		return nil, status.Error(codes.InvalidArgument, "studio_id and month are required")
	}
	if !platform.IsPlatformAdmin(ctx) {
		selectedStudio, err := platform.StudioID(ctx)
		if err != nil || selectedStudio != studioID {
			return nil, status.Error(codes.PermissionDenied, "payroll is limited to the selected authorized studio")
		}
		if platform.HasTenantRole(ctx, "teacher") && !platform.HasTenantRole(ctx, "studio_admin") {
			if teacherID != "" && teacherID != actorID {
				return nil, status.Error(codes.PermissionDenied, "teachers may view only their own payroll")
			}
			teacherID = actorID
		} else if !platform.HasTenantRole(ctx, "studio_admin") {
			return nil, status.Error(codes.PermissionDenied, "teacher or studio administrator role is required")
		}
	}
	month, err := time.Parse("2006-01", monthText)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "month must use YYYY-MM")
	}
	response := &payrollv1.GetMonthlyPayrollResponse{Page: &commonv1.PageResponse{}}
	err = platform.WithTenantTx(ctx, s.db, "payrollservice", func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT class_session_id,teacher_id,approved_duration_minutes,effective_redemption_count FROM payroll.monthly_class_facts WHERE studio_id=$1 AND local_month=$2 AND completed AND ($3='' OR teacher_id::text=$3) ORDER BY teacher_id,class_session_id`, studioID, month, teacherID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			line := &payrollv1.PayrollLine{}
			if err = rows.Scan(&line.ClassSessionId, &line.TeacherId, &line.ApprovedDurationMinutes, &line.RedemptionCount); err != nil {
				return err
			}
			line.BaseAmountCents, line.AttendanceAmountCents, line.TotalAmountCents = calculatePay(int(line.ApprovedDurationMinutes), int(line.RedemptionCount))
			response.TotalAmountCents += line.TotalAmountCents
			response.Lines = append(response.Lines, line)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "unable to calculate payroll")
	}
	return response, nil
}

func calculatePay(approvedMinutes, redemptionCount int) (baseCents, attendanceCents, totalCents int64) {
	baseCents = int64(approvedMinutes) * 3000 / 60
	attendanceCents = int64(redemptionCount) * 400
	return baseCents, attendanceCents, baseCents + attendanceCents
}
