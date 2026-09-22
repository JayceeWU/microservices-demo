using BayAreaDanceHub.Credit.Domain;

namespace BayAreaDanceHub.Credit.Application;

public sealed class CreditApplicationService(ICreditStore store)
{
    public Task<IReadOnlyList<BalanceView>> GetBalances(RequestIdentity identity, CancellationToken ct) => store.GetBalances(identity, ct);
    public Task<IReadOnlyList<GrantSnapshot>> ListGrants(RequestIdentity identity, int limit, CancellationToken ct) => store.ListGrants(identity, limit, ct);
    public Task<IReadOnlyList<LedgerView>> ListLedger(RequestIdentity identity, int limit, CancellationToken ct) => store.ListLedger(identity, limit, ct);
    public Task<HoldView> PlaceHold(RequestIdentity identity, Guid userId, StudioId studioId, BookingId bookingId, CreditAmount amount, DateTimeOffset startsAt, AuditData audit, CancellationToken ct) => store.PlaceHold(identity, userId, studioId, bookingId, amount, startsAt, audit, ct);
    public Task<HoldView> ChangeHold(RequestIdentity identity, Guid holdId, string operation, AuditData audit, CancellationToken ct) => store.ChangeHold(identity, holdId, operation, audit, ct);
    public Task<GrantSnapshot> GrantFromOrder(RequestIdentity identity, NewEntitlement entitlement, AuditData audit, CancellationToken ct) => store.GrantFromOrder(identity, entitlement, audit, ct);
    public Task<RefundEligibilityView> RefundEligibility(RequestIdentity identity, Guid userId, IReadOnlyList<RefundLineRequirement> lines, CancellationToken ct) => store.RefundEligibility(identity, userId, lines, ct);
    public Task<OperationView> ChangeRefund(RequestIdentity identity, Guid userId, Guid attemptId, Guid orderId, IReadOnlyList<RefundLineRequirement> lines, string operation, AuditData audit, CancellationToken ct) => store.ChangeRefund(identity, userId, attemptId, orderId, lines, operation, audit, ct);

    public Task<OperationView> CompensateBooking(RequestIdentity identity, Guid userId, Guid studioId, Guid bookingId, AuditData audit, CancellationToken ct) => store.CompensateBooking(identity, userId, studioId, bookingId, audit, ct);

    public Task<OperationView> Transfer(RequestIdentity identity, GrantId grantId, UserId target, AuditData audit, CancellationToken ct)
    {
        EnsureAdmin(identity);
        return store.Transfer(identity, grantId, target, audit, ct);
    }

    public Task<OperationView> ChangeAvailability(RequestIdentity identity, GrantId grantId, bool pause, AuditData audit, CancellationToken ct)
    {
        EnsureAdmin(identity);
        return store.ChangeAvailability(identity, grantId, pause, audit, ct);
    }

    private static void EnsureAdmin(RequestIdentity identity)
    {
        if (!identity.IsAdmin) throw new UnauthorizedAccessException("studio administrator role required");
    }
}
