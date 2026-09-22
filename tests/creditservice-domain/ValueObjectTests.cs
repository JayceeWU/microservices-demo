using BayAreaDanceHub.Credit.Domain;
using Xunit;

namespace BayAreaDanceHub.Credit.Tests;

public sealed class ValueObjectTests
{
    [Fact]
    public void CreditAmount_rejects_non_positive_units()
    {
        Assert.Throws<ArgumentOutOfRangeException>(() => new CreditAmount(0));
        Assert.Throws<ArgumentOutOfRangeException>(() => new CreditAmount(-1));
    }

    [Fact]
    public void IdempotencyKey_rejects_blank_value()
    {
        Assert.Throws<ArgumentException>(() => new IdempotencyKey(""));
        Assert.Throws<ArgumentException>(() => new IdempotencyKey("   "));
    }

    [Fact]
    public void IdempotencyKey_trims_surrounding_whitespace()
    {
        var key = new IdempotencyKey("  grant:123  ");
        Assert.Equal("grant:123", key.Value);
    }

    [Fact]
    public void GrantId_parse_rejects_non_uuid_text()
    {
        Assert.Throws<ArgumentException>(() => GrantId.Parse("not-a-uuid"));
    }

    [Fact]
    public void BookingId_parse_rejects_non_uuid_text()
    {
        Assert.Throws<ArgumentException>(() => BookingId.Parse("not-a-uuid"));
    }

    [Fact]
    public void ValidityPeriod_treats_null_expiry_as_always_valid()
    {
        var period = new ValidityPeriod(DateTimeOffset.UtcNow.AddDays(-1), null);
        Assert.True(period.IsValidAt(DateTimeOffset.UtcNow.AddYears(10)));
        Assert.Null(period.RemainingSeconds(DateTimeOffset.UtcNow));
    }

    [Fact]
    public void ValidityPeriod_is_invalid_before_start_or_after_expiry()
    {
        var start = new DateTimeOffset(2026, 1, 1, 0, 0, 0, TimeSpan.Zero);
        var period = new ValidityPeriod(start, start.AddDays(10));
        Assert.False(period.IsValidAt(start.AddDays(-1)));
        Assert.True(period.IsValidAt(start.AddDays(5)));
        Assert.False(period.IsValidAt(start.AddDays(10)));
    }
}
