package projection

import (
	"context"

	"github.com/JayceeWU/microservices-demo/dancehub/internal/appcore"
	"github.com/JayceeWU/microservices-demo/dancehub/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Apply(context.Context, Event) error
}

type PostgresStore struct{ Pool *pgxpool.Pool }

func (s PostgresStore) Apply(ctx context.Context, event Event) error {
	actor := appcore.NewActorContext("", "", event.ID, nil, nil, "service", "payroll-projector")
	ctx = platform.WithActor(ctx, actor)
	return platform.WithTenantTx(ctx, s.Pool, "payrollservice", func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `INSERT INTO payroll.inbox_events(event_id,consumer)
			VALUES($1,$2) ON CONFLICT(event_id) DO NOTHING`, event.ID, Group)
		if err != nil || result.RowsAffected() == 0 {
			return err
		}
		f := event.Fact
		// The trigger rejects changes to frozen class identity and conflicting
		// payloads at an equal version. Its error rolls back the inbox as well.
		_, err = tx.Exec(ctx, `INSERT INTO payroll.monthly_class_facts
			(class_session_id,studio_id,teacher_id,local_month,approved_duration_minutes,
			 effective_redemption_count,completed,source_version,studio_timezone)
			VALUES($1,$2,$3,$4,$5,$6,true,$7,$8)
			ON CONFLICT(class_session_id) DO UPDATE SET
			 studio_id=EXCLUDED.studio_id,teacher_id=EXCLUDED.teacher_id,
			 local_month=EXCLUDED.local_month,approved_duration_minutes=EXCLUDED.approved_duration_minutes,
			 effective_redemption_count=EXCLUDED.effective_redemption_count,completed=true,
			 source_version=EXCLUDED.source_version,studio_timezone=EXCLUDED.studio_timezone,updated_at=now()
			WHERE payroll.monthly_class_facts.source_version<=EXCLUDED.source_version`,
			f.ClassSessionID, f.StudioID, f.TeacherID, f.LocalMonth, f.Duration, f.Redemptions, f.Version, f.Timezone)
		return err
	})
}
