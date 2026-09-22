using BayAreaDanceHub.Credit.Domain;
using Xunit;

namespace BayAreaDanceHub.Credit.Tests;

public sealed class CreditDomainTests
{
    private static readonly DateTimeOffset Now = new(2026, 11, 11, 8, 0, 0, TimeSpan.Zero);
    private static readonly StudioId Studio = new(Guid.Parse("11111111-1111-4111-8111-111111111111"));

    [Fact]
    public void Allocation_spans_credits_in_expiry_order()
    {
        var later = Candidate(4, Now.AddDays(10), Studio);
        var earlier = Candidate(2, Now.AddDays(2), Studio);
        var allocations = CreditHold.Allocate([later, earlier], Studio, new CreditAmount(5), Now.AddHours(1));
        Assert.Equal([2, 3], allocations.Select(value => value.ReservedUnits));
        Assert.Equal(-5, allocations.Sum(value => value.CreditDelta));
    }

    [Fact]
    public void Unlimited_grant_preempts_credit_grants()
    {
        var unlimited = Candidate(null, Now.AddDays(30), null, EntitlementKind.Unlimited);
        var credits = Candidate(10, Now.AddDays(1), Studio);
        var allocation = Assert.Single(CreditHold.Allocate([credits, unlimited], Studio, new CreditAmount(4), Now));
        Assert.True(allocation.Unlimited);
        Assert.Equal(0, allocation.CreditDelta);
    }

    [Fact]
    public void Pause_and_resume_preserve_remaining_validity()
    {
        var grant = Candidate(10, Now.AddDays(10), Studio).Grant;
        grant.Pause(Now);
        grant.Resume(Now.AddDays(4));
        Assert.Equal(Now.AddDays(14), grant.Snapshot.Validity.ExpiresAt);
    }

    [Fact]
    public void Refund_eligibility_rejects_final_sales()
    {
        var ordinary = Candidate(10, Now.AddDays(10), Studio).Grant.Snapshot;
        RefundRules.Ensure(new(ordinary, false, false));
        var final = ordinary with { FinalSale = true };
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(final, false, false)));
    }

    [Fact]
    public void Used_final_sale_grant_can_be_transferred_by_domain_command()
    {
        var candidate = Candidate(4, Now.AddDays(10), Studio);
        var usedFinalSale = candidate.Grant.Snapshot with { GrantedCredits = 10, FinalSale = true };
        var source = new CreditGrant(usedFinalSale);
        var target = new UserId(Guid.NewGuid());

        var transfer = source.TransferTo(target, Now);

        Assert.Equal(GrantStatus.Transferred, transfer.Source.Snapshot.Status);
        Assert.Equal(0, transfer.Source.Snapshot.RemainingCredits);
        Assert.Equal(4, transfer.Successor.Snapshot.RemainingCredits);
        Assert.True(transfer.Successor.Snapshot.FinalSale);
        Assert.True(transfer.Successor.Snapshot.NonrefundableAfterTransfer);
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(transfer.Successor.Snapshot, false, false)));
    }

    [Fact]
    public void Hold_requires_exact_allocations_and_enforces_lifecycle()
    {
        var booking = new BookingId(Guid.NewGuid());
        var allocation = new HoldAllocation(new(Guid.NewGuid()), 3, -3, false);
        var hold = new CreditHold(Guid.NewGuid(), booking, new(3), [allocation]);

        hold.Capture();
        hold.Reverse();

        Assert.Equal(CreditHoldState.Reversed, hold.Status);
        Assert.Throws<InvalidGrantTransitionException>(() => hold.Release());
        Assert.Throws<ArgumentException>(() => new CreditHold(Guid.NewGuid(), booking, new(4), [allocation]));
    }

    private static GrantCandidate Candidate(int? credits, DateTimeOffset? expires, StudioId? studio, EntitlementKind kind = EntitlementKind.Credits)
    {
        var snapshot = new GrantSnapshot(new(Guid.NewGuid()), new(Guid.NewGuid()), studio, Guid.NewGuid(), Guid.NewGuid(), kind,
            credits, credits, new(Now.AddDays(-1), expires), GrantStatus.Active, null, null, false, false, null);
        return new(new CreditGrant(snapshot), Now.AddDays(-10));
    }
}
