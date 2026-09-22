using BayAreaDanceHub.Credit.Application;
using BayAreaDanceHub.Credit.Domain;

namespace BayAreaDanceHub.Credit.Infrastructure;

public sealed partial class PostgresCreditStore
{
    public async Task<OperationView> CompensateBooking(RequestIdentity identity, Guid userId, Guid studioId, Guid bookingId, AuditData audit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct, userOverride: userId, studioOverride: studioId);
        await Execute(session, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", ct, "booking:" + bookingId);
        await Execute(session, "INSERT INTO credit.booking_cancellations(booking_id,user_id,studio_id) VALUES($1,$2,$3) ON CONFLICT(booking_id) DO NOTHING", ct, bookingId, userId, studioId);
        await using var command = Command(session, "SELECT id,status FROM credit.holds WHERE booking_id=$1 AND user_id=$2 AND studio_id=$3 FOR UPDATE", bookingId, userId, studioId);
        Guid? holdId = null;
        var state = "";
        await using (var reader = await command.ExecuteReaderAsync(ct))
            if (await reader.ReadAsync(ct)) { holdId = reader.GetGuid(0); state = reader.GetString(1); }
        if (holdId is not null && state is "ACTIVE" or "CAPTURED")
        {
            var allocations = new List<(Guid Grant, int Delta)>();
            await using var allocationCommand = Command(session, "SELECT grant_id,credit_delta FROM credit.hold_allocations WHERE hold_id=$1 ORDER BY grant_id", holdId.Value);
            await using (var reader = await allocationCommand.ExecuteReaderAsync(ct))
                while (await reader.ReadAsync(ct)) allocations.Add((reader.GetGuid(0), -reader.GetInt32(1)));
            foreach (var allocation in allocations)
            {
                if (allocation.Delta != 0) await Execute(session, "UPDATE credit.grants SET remaining_credits=remaining_credits+$1 WHERE id=$2", ct, allocation.Delta, allocation.Grant);
                await InsertLedger(session, userId, studioId, allocation.Grant, holdId, bookingId, state == "ACTIVE" ? "RELEASE" : "REVERSAL", allocation.Delta, $"cancel:{bookingId}:{allocation.Grant}", audit.Reason, null, ct);
            }
            await Execute(session, "UPDATE credit.holds SET status=$2,updated_at=now() WHERE id=$1", ct, holdId.Value, state == "ACTIVE" ? "RELEASED" : "REVERSED");
        }
        await session.CommitAsync(ct);
        return new("COMPENSATE", default, null, "DONE", null);
    }
}
