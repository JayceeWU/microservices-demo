using System.Data;
using BayAreaDanceHub.Credit.Application;
using BayAreaDanceHub.Credit.Domain;

namespace BayAreaDanceHub.Credit.Infrastructure;

public sealed partial class PostgresCreditStore
{
    public async Task<HoldView> PlaceHold(RequestIdentity identity, Guid userId, StudioId studioId, BookingId bookingId, CreditAmount amount, DateTimeOffset classStartsAt, AuditData audit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, IsolationLevel.ReadCommitted, userId, studioId.Value);
        await Execute(session, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", ct, "booking:" + bookingId.Value);
        await using (var cancelled = Command(session, "SELECT EXISTS(SELECT 1 FROM credit.booking_cancellations WHERE booking_id=$1)", bookingId.Value))
            if ((bool)(await cancelled.ExecuteScalarAsync(ct))!) throw new InvalidGrantTransitionException("booking is cancelled");
        var existing = await FindHold(session, bookingId.Value, ct);
        if (existing is not null) { await session.CommitAsync(ct); return existing; }

        await using var select = Command(session, GrantSelect + """
             WHERE user_id=$1 AND status='ACTIVE' AND revoked_at IS NULL AND refund_pending_at IS NULL
               AND valid_from<=$2 AND (expires_at IS NULL OR expires_at>$2)
               AND (studio_id=$3 OR studio_id IS NULL) AND (kind='UNLIMITED' OR remaining_credits>0)
             ORDER BY created_at FOR UPDATE
            """, userId, classStartsAt, studioId.Value);
        var candidates = new List<GrantCandidate>();
        await using (var reader = await select.ExecuteReaderAsync(ct))
            while (await reader.ReadAsync(ct)) candidates.Add(new(new CreditGrant(ReadGrant(reader)), reader.GetFieldValue<DateTimeOffset>(16)));

        var allocations = CreditHold.Allocate(candidates, studioId, amount, classStartsAt);
        var hold = new CreditHold(Guid.NewGuid(), bookingId, amount, allocations);
        await Execute(session, "INSERT INTO credit.holds(id,user_id,studio_id,booking_id,amount,status,class_starts_at) VALUES($1,$2,$3,$4,$5,'ACTIVE',$6)", ct, hold.Id, userId, studioId.Value, bookingId.Value, amount.Units, classStartsAt);
        for (var index = 0; index < allocations.Count; index++)
        {
            var allocation = allocations[index];
            await Execute(session, "INSERT INTO credit.hold_allocations(hold_id,grant_id,reserved_units,credit_delta,unlimited) VALUES($1,$2,$3,$4,$5)", ct, hold.Id, allocation.GrantId.Value, allocation.ReservedUnits, allocation.CreditDelta, allocation.Unlimited);
            if (allocation.CreditDelta != 0)
                await Execute(session, "UPDATE credit.grants SET remaining_credits=remaining_credits+$1 WHERE id=$2 AND remaining_credits>=-$1", ct, allocation.CreditDelta, allocation.GrantId.Value);
            await InsertLedger(session, userId, studioId.Value, allocation.GrantId.Value, hold.Id, bookingId.Value, "HOLD", allocation.CreditDelta, $"{audit.IdempotencyKey}:{index}", DefaultReason(audit, "class booking hold"), null, ct);
        }
        await session.CommitAsync(ct);
        return new(hold.Id, bookingId, amount, hold.Status);
    }

    public async Task<HoldView> ChangeHold(RequestIdentity identity, Guid holdId, string operation, AuditData audit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, IsolationLevel.Serializable);
        await using var select = Command(session, "SELECT user_id,studio_id,booking_id,amount,status FROM credit.holds WHERE id=$1 FOR UPDATE", holdId);
        await using var reader = await select.ExecuteReaderAsync(ct);
        if (!await reader.ReadAsync(ct)) throw new KeyNotFoundException("hold not found");
        var user = reader.GetGuid(0); var studio = reader.GetGuid(1); var booking = new BookingId(reader.GetGuid(2));
        var amount = new CreditAmount(reader.GetInt32(3)); var current = System.Enum.Parse<CreditHoldState>(reader.GetString(4), true);
        await reader.DisposeAsync();
		var target = operation switch { "CAPTURE" => CreditHoldState.Captured, "RELEASE" => CreditHoldState.Released, "REVERSAL" => CreditHoldState.Reversed, _ => throw new ArgumentException("unknown hold operation", nameof(operation)) };
		if (current == target) { await session.CommitAsync(ct); return new(holdId, booking, amount, current); }

        await using var allocationCommand = Command(session, "SELECT grant_id,reserved_units,credit_delta,unlimited FROM credit.hold_allocations WHERE hold_id=$1 ORDER BY grant_id", holdId);
        var allocations = new List<HoldAllocation>();
        await using (var allocationReader = await allocationCommand.ExecuteReaderAsync(ct))
            while (await allocationReader.ReadAsync(ct)) allocations.Add(new(new(allocationReader.GetGuid(0)), allocationReader.GetInt32(1), allocationReader.GetInt32(2), allocationReader.GetBoolean(3)));
        if (allocations.Count == 0) throw new InvalidGrantTransitionException("credit hold has no allocations");

        var aggregate = new CreditHold(holdId, booking, amount, allocations, current);
        switch (operation)
        {
            case "CAPTURE": aggregate.Capture(); break;
            case "RELEASE": aggregate.Release(); break;
            case "REVERSAL": aggregate.Reverse(); break;
			default: throw new ArgumentException("unknown hold operation", nameof(operation));
        }
        await Execute(session, "UPDATE credit.holds SET status=$1,updated_at=now() WHERE id=$2", ct, aggregate.Status.ToString().ToUpperInvariant(), holdId);
        var restore = operation is "RELEASE" or "REVERSAL";
        for (var index = 0; index < allocations.Count; index++)
        {
            var allocation = allocations[index];
            var delta = restore ? -allocation.CreditDelta : 0;
            if (delta != 0) await Execute(session, "UPDATE credit.grants SET remaining_credits=remaining_credits+$1 WHERE id=$2", ct, delta, allocation.GrantId.Value);
            await InsertLedger(session, user, studio, allocation.GrantId.Value, holdId, booking.Value, operation, delta, $"{audit.IdempotencyKey}:{index}", DefaultReason(audit, operation.ToLowerInvariant()), identity.UserId, ct);
        }
        await session.CommitAsync(ct);
        return new(holdId, booking, amount, aggregate.Status);
    }

    private static async Task<HoldView?> FindHold(TenantDbSession session, Guid booking, CancellationToken ct)
    {
        await using var command = Command(session, "SELECT id,booking_id,amount,status FROM credit.holds WHERE booking_id=$1", booking);
        await using var reader = await command.ExecuteReaderAsync(ct);
        return await reader.ReadAsync(ct) ? new(reader.GetGuid(0), new(reader.GetGuid(1)), new(reader.GetInt32(2)), System.Enum.Parse<CreditHoldState>(reader.GetString(3), true)) : null;
    }
}
