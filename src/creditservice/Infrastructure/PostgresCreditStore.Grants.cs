using System.Data;
using BayAreaDanceHub.Credit.Application;
using BayAreaDanceHub.Credit.Domain;

namespace BayAreaDanceHub.Credit.Infrastructure;

public sealed partial class PostgresCreditStore
{
    public async Task<GrantSnapshot> GrantFromOrder(RequestIdentity identity, NewEntitlement value, AuditData audit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, IsolationLevel.Serializable, value.UserId, value.StudioId);
        var now = DateTimeOffset.UtcNow; DateTimeOffset? expires = value.ValidityDays > 0 ? now.AddDays(value.ValidityDays) : null; var id = Guid.NewGuid();
        await using var command = Command(session, """
            INSERT INTO credit.grants(id,user_id,studio_id,source_order_line_id,fulfillment_key,product_version_id,kind,granted_credits,remaining_credits,valid_from,expires_at,final_sale)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$10,$11)
            ON CONFLICT(fulfillment_key) DO UPDATE SET fulfillment_key=EXCLUDED.fulfillment_key RETURNING id
            """, id, value.UserId, (object?)value.StudioId ?? DBNull.Value, value.OrderLineId, value.FulfillmentKey, value.ProductVersionId, KindText(value.Kind), (object?)value.CreditAmount ?? DBNull.Value, now, (object?)expires ?? DBNull.Value, value.FinalSale);
        id = (Guid)(await command.ExecuteScalarAsync(ct) ?? id);
        await InsertLedger(session, value.UserId, value.StudioId, id, null, null, "GRANT", value.CreditAmount ?? 0, audit.IdempotencyKey.ToString(), "membership product purchase", null, ct);
        await session.CommitAsync(ct);
        return new(new(id), new(value.UserId), value.StudioId is null ? null : new StudioId(value.StudioId.Value), value.OrderLineId, value.ProductVersionId, value.Kind, value.CreditAmount, value.CreditAmount, new(now, expires), GrantStatus.Active, null, null, value.FinalSale, false, null);
    }

    public async Task<OperationView> Transfer(RequestIdentity identity, GrantId grantId, UserId target, AuditData audit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, IsolationLevel.Serializable);
        var before = await LoadGrant(session, grantId.Value, true, ct); EnsureStudio(identity, before.StudioId);
        if (before.RefundPendingAt is not null) throw new RefundDeniedException("grant is frozen for refund");
        var transfer = new CreditGrant(before).TransferTo(target, DateTimeOffset.UtcNow); var source = transfer.Source.Snapshot; var successor = transfer.Successor.Snapshot;
        await Execute(session, """
            INSERT INTO credit.grants(id,user_id,studio_id,source_order_line_id,fulfillment_key,product_version_id,kind,granted_credits,remaining_credits,valid_from,expires_at,status,paused_at,remaining_validity_seconds,predecessor_grant_id,final_sale,nonrefundable_after_transfer)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,true)
            """, ct, successor.Id.Value, successor.UserId.Value, (object?)successor.StudioId?.Value ?? DBNull.Value, successor.SourceOrderLineId, $"transfer:{successor.Id.Value}", successor.ProductVersionId, KindText(successor.Kind), (object?)successor.GrantedCredits ?? DBNull.Value, (object?)successor.RemainingCredits ?? DBNull.Value, successor.Validity.ValidFrom, (object?)successor.Validity.ExpiresAt ?? DBNull.Value, StatusText(successor.Status), (object?)successor.PausedAt ?? DBNull.Value, (object?)successor.RemainingValiditySeconds ?? DBNull.Value, grantId.Value, successor.FinalSale);
        await Execute(session, "UPDATE credit.grants SET status='TRANSFERRED',transferred_at=now(),remaining_credits=CASE WHEN remaining_credits IS NULL THEN NULL ELSE 0 END,nonrefundable_after_transfer=true WHERE id=$1", ct, grantId.Value);
        await InsertOperation(session, grantId.Value, successor.Id.Value, "TRANSFER", source.UserId.Value, target.Value, source.StudioId?.Value, identity.UserId, audit.Reason, StatusText(before.Status), "TRANSFERRED", audit.IdempotencyKey.ToString(), identity.RequestId, ct);
        await session.CommitAsync(ct); return new("TRANSFER", grantId, successor.Id, "TRANSFERRED", successor.Validity.ExpiresAt);
    }

    public async Task<OperationView> ChangeAvailability(RequestIdentity identity, GrantId grantId, bool pause, AuditData audit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, IsolationLevel.Serializable);
        var before = await LoadGrant(session, grantId.Value, true, ct); EnsureStudio(identity, before.StudioId); var aggregate = new CreditGrant(before);
        if (before.RefundPendingAt is not null) throw new RefundDeniedException("grant is frozen for refund");
        if (pause) aggregate.Pause(DateTimeOffset.UtcNow); else aggregate.Resume(DateTimeOffset.UtcNow); var after = aggregate.Snapshot;
        await Execute(session, "UPDATE credit.grants SET status=$2,paused_at=$3,remaining_validity_seconds=$4,expires_at=$5 WHERE id=$1", ct, grantId.Value, StatusText(after.Status), (object?)after.PausedAt ?? DBNull.Value, (object?)after.RemainingValiditySeconds ?? DBNull.Value, (object?)after.Validity.ExpiresAt ?? DBNull.Value);
        var operation = pause ? "PAUSE" : "RESUME";
        await InsertOperation(session, grantId.Value, null, operation, before.UserId.Value, null, before.StudioId?.Value, identity.UserId, audit.Reason, StatusText(before.Status), StatusText(after.Status), audit.IdempotencyKey.ToString(), identity.RequestId, ct);
        await session.CommitAsync(ct); return new(operation, grantId, null, StatusText(after.Status), after.Validity.ExpiresAt);
    }
}
