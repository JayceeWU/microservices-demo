namespace BayAreaDanceHub.Credit.Domain;

public sealed record GrantCandidate(CreditGrant Grant, DateTimeOffset CreatedAt);
public sealed record HoldAllocation(GrantId GrantId, int ReservedUnits, int CreditDelta, bool Unlimited);
public enum CreditHoldState { Active, Captured, Released, Reversed }

public sealed class CreditHold
{
    public Guid Id { get; }
    public BookingId BookingId { get; }
    public CreditAmount Amount { get; }
    public IReadOnlyList<HoldAllocation> Allocations { get; }
    public CreditHoldState Status { get; private set; }

    public CreditHold(Guid id, BookingId bookingId, CreditAmount amount, IReadOnlyList<HoldAllocation> allocations, CreditHoldState status = CreditHoldState.Active)
    {
        if (allocations.Sum(value => value.ReservedUnits) != amount.Units)
            throw new ArgumentException("hold allocations must cover the requested amount", nameof(allocations));
        Id = id; BookingId = bookingId; Amount = amount; Allocations = allocations; Status = status;
    }

    public static IReadOnlyList<HoldAllocation> Allocate(IEnumerable<GrantCandidate> candidates, StudioId studio, CreditAmount amount, DateTimeOffset classStartsAt)
    {
        var applicable = candidates
            .Where(candidate => candidate.Grant.Snapshot.Status == GrantStatus.Active)
            .Where(candidate => candidate.Grant.Snapshot.RefundPendingAt is null)
            .Where(candidate => candidate.Grant.Snapshot.Validity.IsValidAt(classStartsAt))
            .Where(candidate => candidate.Grant.Snapshot.StudioId is null || candidate.Grant.Snapshot.StudioId == studio)
            .OrderBy(candidate => Priority(candidate.Grant.Snapshot, studio))
            .ThenBy(candidate => candidate.Grant.Snapshot.Validity.ExpiresAt ?? DateTimeOffset.MaxValue)
            .ThenBy(candidate => candidate.CreatedAt)
            .ToArray();

        var unlimited = applicable.FirstOrDefault(candidate => candidate.Grant.Snapshot.Kind == EntitlementKind.Unlimited);
        if (unlimited is not null)
            return [new(unlimited.Grant.Snapshot.Id, amount.Units, 0, true)];

        var remaining = amount.Units;
        var result = new List<HoldAllocation>();
        foreach (var candidate in applicable)
        {
            if (remaining == 0) break;
            var units = Math.Min(remaining, candidate.Grant.Snapshot.RemainingCredits ?? 0);
            var allocation = new HoldAllocation(candidate.Grant.Snapshot.Id, units, -units, false);
            if (allocation.ReservedUnits == 0) continue;
            result.Add(allocation);
            remaining -= allocation.ReservedUnits;
        }
        if (remaining != 0) throw new InsufficientCreditException("insufficient applicable credits");
        return result;
    }

    private static int Priority(GrantSnapshot grant, StudioId studio) => grant.Kind switch
    {
        EntitlementKind.Unlimited => 0,
        EntitlementKind.Credits when grant.StudioId == studio => 1,
        EntitlementKind.Credits when grant.Validity.ExpiresAt is not null => 2,
        _ => 3
    };
    public void Capture() => Transition(CreditHoldState.Active, CreditHoldState.Captured);
    public void Release() => Transition(CreditHoldState.Active, CreditHoldState.Released);
    public void Reverse() => Transition(CreditHoldState.Captured, CreditHoldState.Reversed);
    private void Transition(CreditHoldState expected, CreditHoldState next)
    {
        if (Status != expected) throw new InvalidGrantTransitionException($"credit hold must be {expected} before {next}");
        Status = next;
    }
}
