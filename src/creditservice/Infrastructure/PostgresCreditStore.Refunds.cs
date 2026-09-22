using System.Data;
using System.Text.Json;
using BayAreaDanceHub.Credit.Application;
using BayAreaDanceHub.Credit.Domain;

namespace BayAreaDanceHub.Credit.Infrastructure;

public sealed partial class PostgresCreditStore
{
    private static string Requirements(IReadOnlyList<RefundLineRequirement> lines)
    {
        if (lines.Count == 0 || lines.Any(l => l.Quantity < 1 || l.Quantity > 20) || lines.Select(l => l.OrderLineId).Distinct().Count() != lines.Count)
            throw new ArgumentException("unique order lines and quantities between 1 and 20 are required");
        return JsonSerializer.Serialize(lines.OrderBy(l => l.OrderLineId));
    }

    private static async Task<List<GrantSnapshot>> RefundGrants(TenantDbSession session, Guid user, IReadOnlyList<RefundLineRequirement> lines, CancellationToken ct)
    {
        await using var command = Command(session, GrantSelect + " WHERE user_id=$1 AND source_order_line_id=ANY($2) ORDER BY id FOR UPDATE", user, lines.Select(l => l.OrderLineId).ToArray());
        var grants = new List<GrantSnapshot>();
        await using (var reader = await command.ExecuteReaderAsync(ct))
            while (await reader.ReadAsync(ct)) grants.Add(ReadGrant(reader));
        foreach (var line in lines)
        {
            await using var keys = Command(session, "SELECT fulfillment_key FROM credit.grants WHERE user_id=$1 AND source_order_line_id=$2 ORDER BY fulfillment_key", user, line.OrderLineId);
            var actual = new HashSet<string>();
            await using (var reader = await keys.ExecuteReaderAsync(ct)) while (await reader.ReadAsync(ct)) actual.Add(reader.GetString(0));
            if (!actual.SetEquals(Enumerable.Range(0, line.Quantity).Select(i => $"{line.OrderLineId}:{i}")))
                throw new RefundDeniedException("grant_missing_or_transferred");
        }
        return grants;
    }

    private static async Task CheckRefund(TenantDbSession session, IEnumerable<GrantSnapshot> grants, CancellationToken ct)
    {
        foreach (var grant in grants)
        {
            if (grant.Status is not (GrantStatus.Active or GrantStatus.Paused) || grant.RefundPendingAt is not null)
                throw new RefundDeniedException("grant is unavailable for refund");
            await using var usage = Command(session, "SELECT EXISTS(SELECT 1 FROM credit.hold_allocations a JOIN credit.holds h ON h.id=a.hold_id WHERE a.grant_id=$1 AND h.status='ACTIVE'), EXISTS(SELECT 1 FROM credit.hold_allocations a JOIN credit.holds h ON h.id=a.hold_id WHERE a.grant_id=$1 AND h.status='CAPTURED')", grant.Id.Value);
            await using var reader = await usage.ExecuteReaderAsync(ct);
            await reader.ReadAsync(ct);
            RefundRules.Ensure(new(grant, reader.GetBoolean(0), reader.GetBoolean(1)));
        }
    }

    public async Task<RefundEligibilityView> RefundEligibility(RequestIdentity identity, Guid userId, IReadOnlyList<RefundLineRequirement> lines, CancellationToken ct)
    {
        Requirements(lines);
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, userOverride: userId);
        try
        {
            await CheckRefund(session, await RefundGrants(session, userId, lines, ct), ct);
            await session.CommitAsync(ct);
            return new(true, "");
        }
        catch (RefundDeniedException error) { return new(false, error.Message); }
    }

    public async Task<OperationView> ChangeRefund(RequestIdentity identity, Guid userId, Guid attemptId, Guid orderId, IReadOnlyList<RefundLineRequirement> lines, string operation, AuditData audit, CancellationToken ct)
    {
        var requirements = Requirements(lines);
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, IsolationLevel.ReadCommitted, userId);
        await Execute(session, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", ct, "refund:" + attemptId);
        await using var get = Command(session, "SELECT status, user_id, order_id, requirements::text FROM credit.refund_attempts WHERE id=$1 FOR UPDATE", attemptId);
        string? state = null;
        await using (var reader = await get.ExecuteReaderAsync(ct))
        {
            if (await reader.ReadAsync(ct))
            {
                state = reader.GetString(0);
                if (reader.GetGuid(1) != userId || reader.GetGuid(2) != orderId || JsonSerializer.Serialize(JsonSerializer.Deserialize<RefundLineRequirement[]>(reader.GetString(3))) != requirements)
                    throw new InvalidGrantTransitionException("refund attempt payload differs");
            }
        }
        var target = operation switch { "RESERVE" => "RESERVED", "RELEASE" => "RELEASED", "REVOKE" => "REVOKED", _ => throw new ArgumentException("invalid refund operation") };
        if (state == target) { await session.CommitAsync(ct); return new(operation, default, null, target, null); }
        if (operation == "RESERVE")
        {
            if (state is not null) throw new InvalidGrantTransitionException("refund attempt is already closed");
            var grants = await RefundGrants(session, userId, lines, ct);
            await CheckRefund(session, grants, ct);
            await Execute(session, "INSERT INTO credit.refund_attempts(id,order_id,user_id,requirements,status) VALUES($1,$2,$3,$4::jsonb,'RESERVED')", ct, attemptId, orderId, userId, requirements);
            foreach (var grant in grants)
                await Execute(session, "UPDATE credit.grants SET refund_pending_at=now(),refund_attempt_id=$2 WHERE id=$1", ct, grant.Id.Value, attemptId);
        }
        else
        {
            if (state != "RESERVED") throw new InvalidGrantTransitionException("refund must be reserved before completion");
            var grants = await RefundGrants(session, userId, lines, ct);
            await using var count = Command(session, "SELECT count(*) FROM credit.grants WHERE user_id=$1 AND refund_attempt_id=$2 AND refund_pending_at IS NOT NULL", userId, attemptId);
            if (Convert.ToInt64(await count.ExecuteScalarAsync(ct)) != grants.Count)
                throw new InvalidGrantTransitionException("refund reservation is incomplete");
            foreach (var grant in grants)
            {
                if (operation == "REVOKE")
                {
                    await Execute(session, "UPDATE credit.grants SET revoked_at=now(),status='REVOKED',remaining_credits=CASE WHEN remaining_credits IS NULL THEN NULL ELSE 0 END,refund_pending_at=NULL WHERE id=$1", ct, grant.Id.Value);
                    await InsertLedger(session, userId, grant.StudioId?.Value, grant.Id.Value, null, null, "REFUND", -(grant.RemainingCredits ?? 0), $"refund:{attemptId}:{grant.Id.Value}", audit.Reason, null, ct);
                }
                else await Execute(session, "UPDATE credit.grants SET refund_pending_at=NULL,refund_attempt_id=NULL WHERE id=$1", ct, grant.Id.Value);
            }
            await Execute(session, "UPDATE credit.refund_attempts SET status=$2 WHERE id=$1", ct, attemptId, target);
        }
        await session.CommitAsync(ct);
        return new(operation, default, null, target, null);
    }
}
