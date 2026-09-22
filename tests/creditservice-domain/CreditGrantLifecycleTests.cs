using BayAreaDanceHub.Credit.Domain;
using Xunit;

namespace BayAreaDanceHub.Credit.Tests;

public sealed class CreditGrantLifecycleTests
{
    private static readonly DateTimeOffset Now = new(2026, 11, 11, 8, 0, 0, TimeSpan.Zero);

    private static GrantSnapshot Snapshot(GrantStatus status = GrantStatus.Active, int? remaining = 10, DateTimeOffset? expiresAt = null) =>
        new(new(Guid.NewGuid()), new(Guid.NewGuid()), null, Guid.NewGuid(), Guid.NewGuid(), EntitlementKind.Credits,
            remaining, remaining, new(Now.AddDays(-1), expiresAt ?? Now.AddDays(30)), status, null, null, false, false, null);

    [Fact]
    public void Constructor_rejects_negative_remaining_credits()
    {
        var invalid = Snapshot() with { RemainingCredits = -1 };
        Assert.Throws<ArgumentOutOfRangeException>(() => new CreditGrant(invalid));
    }

    [Fact]
    public void Constructor_requires_balance_for_credits_kind()
    {
        var invalid = Snapshot() with { RemainingCredits = null };
        Assert.Throws<ArgumentException>(() => new CreditGrant(invalid));
    }

    [Fact]
    public void Pause_rejects_non_active_grant()
    {
        var grant = new CreditGrant(Snapshot(GrantStatus.Paused));
        Assert.Throws<InvalidGrantTransitionException>(() => grant.Pause(Now));
    }

    [Fact]
    public void Resume_rejects_non_paused_grant()
    {
        var grant = new CreditGrant(Snapshot(GrantStatus.Active));
        Assert.Throws<InvalidGrantTransitionException>(() => grant.Resume(Now));
    }

    [Fact]
    public void TransferTo_rejects_revoked_or_transferred_grant()
    {
        var revoked = new CreditGrant(Snapshot(GrantStatus.Revoked));
        Assert.Throws<InvalidGrantTransitionException>(() => revoked.TransferTo(new UserId(Guid.NewGuid()), Now));

        var transferred = new CreditGrant(Snapshot(GrantStatus.Transferred));
        Assert.Throws<InvalidGrantTransitionException>(() => transferred.TransferTo(new UserId(Guid.NewGuid()), Now));
    }

    [Fact]
    public void TransferTo_rejects_transfer_to_self()
    {
        var snapshot = Snapshot();
        var grant = new CreditGrant(snapshot);
        Assert.Throws<InvalidGrantTransitionException>(() => grant.TransferTo(snapshot.UserId, Now));
    }

    [Fact]
    public void Refund_reservation_can_be_released_without_revoking()
    {
        var grant = new CreditGrant(Snapshot());
        grant.ReserveRefund(Now);
        Assert.Equal(Now, grant.Snapshot.RefundPendingAt);

        grant.ReleaseRefund();
        Assert.Null(grant.Snapshot.RefundPendingAt);
        Assert.Equal(GrantStatus.Active, grant.Snapshot.Status);
    }

    [Fact]
    public void ReserveRefund_rejects_non_active_grant()
    {
        var grant = new CreditGrant(Snapshot(GrantStatus.Paused));
        Assert.Throws<InvalidGrantTransitionException>(() => grant.ReserveRefund(Now));
    }

    [Fact]
    public void RevokeAfterRefund_requires_reserved_refund()
    {
        var grant = new CreditGrant(Snapshot());
        Assert.Throws<InvalidGrantTransitionException>(() => grant.RevokeAfterRefund());
    }

    [Fact]
    public void RevokeAfterRefund_zeroes_balance_and_marks_revoked()
    {
        var grant = new CreditGrant(Snapshot(remaining: 6));
        grant.ReserveRefund(Now);

        grant.RevokeAfterRefund();

        Assert.Equal(GrantStatus.Revoked, grant.Snapshot.Status);
        Assert.Equal(0, grant.Snapshot.RemainingCredits);
        Assert.Null(grant.Snapshot.RefundPendingAt);
    }
}
