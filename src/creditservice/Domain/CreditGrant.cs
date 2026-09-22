namespace BayAreaDanceHub.Credit.Domain;

public enum GrantStatus { Active, Paused, Transferred, Revoked }
public enum EntitlementKind { Credits, Unlimited }

public sealed record GrantSnapshot(
    GrantId Id,
    UserId UserId,
    StudioId? StudioId,
    Guid SourceOrderLineId,
    Guid ProductVersionId,
    EntitlementKind Kind,
    int? GrantedCredits,
    int? RemainingCredits,
    ValidityPeriod Validity,
    GrantStatus Status,
    DateTimeOffset? PausedAt,
    long? RemainingValiditySeconds,
    bool FinalSale,
    bool NonrefundableAfterTransfer,
    DateTimeOffset? RefundPendingAt);

public sealed record GrantTransfer(CreditGrant Source, CreditGrant Successor);

public sealed class CreditGrant
{
    private GrantSnapshot _state;
    public GrantSnapshot Snapshot => _state;

    public CreditGrant(GrantSnapshot snapshot)
    {
        if (snapshot.RemainingCredits < 0) throw new ArgumentOutOfRangeException(nameof(snapshot), "remaining credits cannot be negative");
        if (snapshot.Kind == EntitlementKind.Credits && snapshot.RemainingCredits is null)
            throw new ArgumentException("credits grant requires a balance", nameof(snapshot));
        _state = snapshot;
    }

    public void Pause(DateTimeOffset now)
    {
        Require(GrantStatus.Active, "only an active grant can be paused");
        EnsureNotRefunding();
        _state = _state with
        {
            Status = GrantStatus.Paused,
            PausedAt = now,
            RemainingValiditySeconds = _state.Validity.RemainingSeconds(now)
        };
    }

    public void Resume(DateTimeOffset now)
    {
        Require(GrantStatus.Paused, "only a paused grant can be resumed");
        EnsureNotRefunding();
        DateTimeOffset? expiresAt = _state.Validity.ExpiresAt is null
            ? null
            : now.AddSeconds(_state.RemainingValiditySeconds ?? 0);
        _state = _state with
        {
            Status = GrantStatus.Active,
            PausedAt = null,
            RemainingValiditySeconds = null,
            Validity = new ValidityPeriod(_state.Validity.ValidFrom, expiresAt)
        };
    }

    public GrantTransfer TransferTo(UserId target, DateTimeOffset now)
    {
        EnsureNotRefunding();
        if (_state.Status is not (GrantStatus.Active or GrantStatus.Paused))
            throw new InvalidGrantTransitionException("only active or paused grants can be transferred");
        if (target == _state.UserId) throw new InvalidGrantTransitionException("source and target users must differ");

        var successor = new CreditGrant(_state with
        {
            Id = new GrantId(Guid.NewGuid()),
            UserId = target,
            NonrefundableAfterTransfer = true,
            RefundPendingAt = null
        });
        _state = _state with
        {
            Status = GrantStatus.Transferred,
            RemainingCredits = _state.RemainingCredits is null ? null : 0,
            NonrefundableAfterTransfer = true,
            RefundPendingAt = null
        };
        return new GrantTransfer(this, successor);
    }

    public void ReserveRefund(DateTimeOffset now)
    {
        Require(GrantStatus.Active, "only an active grant can be reserved for refund");
        _state = _state with { RefundPendingAt = now };
    }

    public void ReleaseRefund() => _state = _state with { RefundPendingAt = null };

    public void RevokeAfterRefund()
    {
        if (_state.RefundPendingAt is null) throw new InvalidGrantTransitionException("refund must be reserved before revocation");
        _state = _state with { Status = GrantStatus.Revoked, RemainingCredits = _state.RemainingCredits is null ? null : 0, RefundPendingAt = null };
    }

    private void EnsureNotRefunding() { if (_state.RefundPendingAt is not null) throw new RefundDeniedException("grant is frozen for refund"); }

    private void Require(GrantStatus expected, string message)
    {
        if (_state.Status != expected) throw new InvalidGrantTransitionException(message);
    }
}
