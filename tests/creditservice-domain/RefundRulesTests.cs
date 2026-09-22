using BayAreaDanceHub.Credit.Domain;
using Xunit;

namespace BayAreaDanceHub.Credit.Tests;

public sealed class RefundRulesTests
{
    private static readonly DateTimeOffset Now = new(2026, 11, 11, 8, 0, 0, TimeSpan.Zero);

    private static GrantSnapshot Ordinary() =>
        new(new(Guid.NewGuid()), new(Guid.NewGuid()), null, Guid.NewGuid(), Guid.NewGuid(), EntitlementKind.Credits,
            10, 10, new(Now.AddDays(-1), Now.AddDays(30)), GrantStatus.Active, null, null, false, false, null);

    [Fact]
    public void Ensure_allows_ordinary_grant_with_no_hold_or_use()
    {
        RefundRules.Ensure(new(Ordinary(), false, false));
    }

    [Fact]
    public void Ensure_rejects_grant_with_active_hold()
    {
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(Ordinary(), true, false)));
    }

    [Fact]
    public void Ensure_rejects_grant_with_unreversed_use()
    {
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(Ordinary(), false, true)));
    }

    [Fact]
    public void Ensure_rejects_transferred_grant_even_without_hold_or_use()
    {
        var transferred = Ordinary() with { Status = GrantStatus.Transferred };
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(transferred, false, false)));
    }

    [Fact]
    public void Ensure_rejects_grant_marked_nonrefundable_after_transfer()
    {
        var successor = Ordinary() with { NonrefundableAfterTransfer = true };
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(successor, false, false)));
    }
    [Fact]
    public void Paused_unused_grants_are_refundable_but_revoked_grants_are_not()
    {
        RefundRules.Ensure(new(Ordinary() with { Status = GrantStatus.Paused }, false, false));
        Assert.Throws<RefundDeniedException>(() => RefundRules.Ensure(new(Ordinary() with { Status = GrantStatus.Revoked }, false, false)));
    }

    [Fact]
    public void Refund_freeze_prevents_transfer_pause_and_resume()
    {
        var frozen = Ordinary() with { RefundPendingAt = Now };
        Assert.Throws<RefundDeniedException>(() => new CreditGrant(frozen).TransferTo(new UserId(Guid.NewGuid()), Now));
        Assert.Throws<RefundDeniedException>(() => new CreditGrant(frozen).Pause(Now));
        Assert.Throws<RefundDeniedException>(() => new CreditGrant(frozen with { Status = GrantStatus.Paused }).Resume(Now));
    }
}
