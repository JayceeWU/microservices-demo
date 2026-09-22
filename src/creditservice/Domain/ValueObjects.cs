namespace BayAreaDanceHub.Credit.Domain;

public readonly record struct GrantId(Guid Value)
{
    public static GrantId Parse(string value) => Guid.TryParse(value, out var id)
        ? new(id) : throw new ArgumentException("grant id must be a UUID", nameof(value));
    public override string ToString() => Value.ToString();
}

public readonly record struct BookingId(Guid Value)
{
    public static BookingId Parse(string value) => Guid.TryParse(value, out var id)
        ? new(id) : throw new ArgumentException("booking id must be a UUID", nameof(value));
}

public readonly record struct StudioId(Guid Value)
{
    public override string ToString() => Value.ToString();
}
public readonly record struct UserId(Guid Value);

public readonly record struct CreditAmount
{
    public int Units { get; }
    public CreditAmount(int units)
    {
        if (units <= 0) throw new ArgumentOutOfRangeException(nameof(units), "credit amount must be positive");
        Units = units;
    }
}

public readonly record struct IdempotencyKey
{
    public string Value { get; }
    public IdempotencyKey(string value)
    {
        if (string.IsNullOrWhiteSpace(value)) throw new ArgumentException("idempotency key is required", nameof(value));
        Value = value.Trim();
    }
    public override string ToString() => Value;
}

public readonly record struct ValidityPeriod(DateTimeOffset ValidFrom, DateTimeOffset? ExpiresAt)
{
    public bool IsValidAt(DateTimeOffset moment) => ValidFrom <= moment && (ExpiresAt is null || ExpiresAt > moment);
    public long? RemainingSeconds(DateTimeOffset moment) => ExpiresAt is null ? null : Math.Max(0, (long)(ExpiresAt.Value - moment).TotalSeconds);
}
