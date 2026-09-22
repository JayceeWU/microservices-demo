using BayAreaDanceHub.Credit.Domain;
using Xunit;

namespace BayAreaDanceHub.Credit.Tests;

public sealed class CreditHoldTests
{
    private static readonly DateTimeOffset Now = new(2026, 11, 11, 8, 0, 0, TimeSpan.Zero);
    private static readonly StudioId Studio = new(Guid.Parse("11111111-1111-4111-8111-111111111111"));
    private static readonly StudioId OtherStudio = new(Guid.Parse("22222222-2222-4222-8222-222222222222"));

    private static GrantCandidate Candidate(int? credits, DateTimeOffset? expires, StudioId? studio,
        GrantStatus status = GrantStatus.Active, DateTimeOffset? refundPendingAt = null)
    {
        var snapshot = new GrantSnapshot(new(Guid.NewGuid()), new(Guid.NewGuid()), studio, Guid.NewGuid(), Guid.NewGuid(), EntitlementKind.Credits,
            credits, credits, new(Now.AddDays(-30), expires), status, null, null, false, false, refundPendingAt);
        return new(new CreditGrant(snapshot), Now.AddDays(-10));
    }

    [Fact]
    public void Allocate_excludes_paused_grants()
    {
        var paused = Candidate(10, Now.AddDays(10), Studio, status: GrantStatus.Paused);
        Assert.Throws<InsufficientCreditException>(() => CreditHold.Allocate([paused], Studio, new CreditAmount(1), Now));
    }

    [Fact]
    public void Allocate_excludes_grants_with_pending_refund()
    {
        var pending = Candidate(10, Now.AddDays(10), Studio, refundPendingAt: Now.AddMinutes(-1));
        Assert.Throws<InsufficientCreditException>(() => CreditHold.Allocate([pending], Studio, new CreditAmount(1), Now));
    }

    [Fact]
    public void Allocate_excludes_grants_already_expired_at_class_start()
    {
        var expired = Candidate(10, Now.AddDays(-1), Studio);
        Assert.Throws<InsufficientCreditException>(() => CreditHold.Allocate([expired], Studio, new CreditAmount(1), Now));
    }

    [Fact]
    public void Allocate_excludes_grants_scoped_to_a_different_studio()
    {
        var other = Candidate(10, Now.AddDays(10), OtherStudio);
        Assert.Throws<InsufficientCreditException>(() => CreditHold.Allocate([other], Studio, new CreditAmount(1), Now));
    }

    [Fact]
    public void Allocate_throws_when_no_grant_covers_the_requested_amount()
    {
        var small = Candidate(2, Now.AddDays(10), Studio);
        Assert.Throws<InsufficientCreditException>(() => CreditHold.Allocate([small], Studio, new CreditAmount(5), Now));
    }

    [Fact]
    public void Release_from_active_succeeds()
    {
        var allocation = new HoldAllocation(new(Guid.NewGuid()), 3, -3, false);
        var hold = new CreditHold(Guid.NewGuid(), new(Guid.NewGuid()), new(3), [allocation]);

        hold.Release();

        Assert.Equal(CreditHoldState.Released, hold.Status);
    }

    [Fact]
    public void Capture_then_release_is_rejected()
    {
        var allocation = new HoldAllocation(new(Guid.NewGuid()), 3, -3, false);
        var hold = new CreditHold(Guid.NewGuid(), new(Guid.NewGuid()), new(3), [allocation]);
        hold.Capture();

        Assert.Throws<InvalidGrantTransitionException>(() => hold.Release());
    }

    [Fact]
    public void Reverse_without_capture_is_rejected()
    {
        var allocation = new HoldAllocation(new(Guid.NewGuid()), 3, -3, false);
        var hold = new CreditHold(Guid.NewGuid(), new(Guid.NewGuid()), new(3), [allocation]);

        Assert.Throws<InvalidGrantTransitionException>(() => hold.Reverse());
    }

    [Fact]
    public void Capture_twice_is_rejected()
    {
        var allocation = new HoldAllocation(new(Guid.NewGuid()), 3, -3, false);
        var hold = new CreditHold(Guid.NewGuid(), new(Guid.NewGuid()), new(3), [allocation]);
        hold.Capture();

        Assert.Throws<InvalidGrantTransitionException>(() => hold.Capture());
    }
}
