using BayAreaDanceHub.Credit.Domain;

namespace BayAreaDanceHub.Credit.Application;

public sealed record RequestIdentity(
    Guid UserId,
    Guid? StudioId,
    IReadOnlySet<string> TenantRoles,
    string RequestId,
    IReadOnlySet<string> GlobalRoles,
    string ActorKind,
    string ServicePrincipal)
{
    public bool IsPlatformAdmin => ActorKind == "human" && GlobalRoles.Contains("platform_admin");
    public bool IsAdmin => TenantRoles.Contains("studio_admin") || IsPlatformAdmin;
}

public sealed record AuditData(IdempotencyKey IdempotencyKey, string Reason);
public sealed record BalanceView(Guid? StudioId, int AvailableCredits, bool UnlimitedActive);
public sealed record LedgerView(Guid Id, Guid? StudioId, GrantId GrantId, Guid? BookingId, string Kind, int CreditDelta, string Reason, DateTimeOffset CreatedAt);
public sealed record HoldView(Guid Id, BookingId BookingId, CreditAmount Amount, CreditHoldState Status);
public sealed record OperationView(string Operation, GrantId GrantId, GrantId? SuccessorGrantId, string Status, DateTimeOffset? ExpiresAt);
public sealed record RefundLineRequirement(Guid OrderLineId, int Quantity);
public sealed record RefundEligibilityView(bool Eligible, string Reason);
public sealed record NewEntitlement(Guid OrderLineId, string FulfillmentKey, Guid ProductVersionId, Guid UserId, Guid? StudioId, EntitlementKind Kind, int? CreditAmount, int ValidityDays, bool FinalSale);

public interface ICreditStore
{
    Task<IReadOnlyList<BalanceView>> GetBalances(RequestIdentity identity, CancellationToken cancellationToken);
    Task<IReadOnlyList<GrantSnapshot>> ListGrants(RequestIdentity identity, int limit, CancellationToken cancellationToken);
    Task<IReadOnlyList<LedgerView>> ListLedger(RequestIdentity identity, int limit, CancellationToken cancellationToken);
    Task<HoldView> PlaceHold(RequestIdentity identity, Guid userId, StudioId studioId, BookingId bookingId, CreditAmount amount, DateTimeOffset classStartsAt, AuditData audit, CancellationToken cancellationToken);
    Task<HoldView> ChangeHold(RequestIdentity identity, Guid holdId, string operation, AuditData audit, CancellationToken cancellationToken);
    Task<GrantSnapshot> GrantFromOrder(RequestIdentity identity, NewEntitlement entitlement, AuditData audit, CancellationToken cancellationToken);
    Task<RefundEligibilityView> RefundEligibility(RequestIdentity identity, Guid userId, IReadOnlyList<RefundLineRequirement> requirements, CancellationToken cancellationToken);
    Task<OperationView> ChangeRefund(RequestIdentity identity, Guid userId, Guid attemptId, Guid orderId, IReadOnlyList<RefundLineRequirement> requirements, string operation, AuditData audit, CancellationToken cancellationToken);
    Task<OperationView> CompensateBooking(RequestIdentity identity, Guid userId, Guid studioId, Guid bookingId, AuditData audit, CancellationToken cancellationToken);
    Task<OperationView> Transfer(RequestIdentity identity, GrantId grantId, UserId target, AuditData audit, CancellationToken cancellationToken);
    Task<OperationView> ChangeAvailability(RequestIdentity identity, GrantId grantId, bool pause, AuditData audit, CancellationToken cancellationToken);
}
