namespace BayAreaDanceHub.Credit.Domain;

public sealed record RefundContext(GrantSnapshot Grant, bool HasActiveHold, bool HasUnreversedUse);
public static class RefundRules
{
    public static void Ensure(RefundContext context)
    {
        if (context.Grant.Status is not (GrantStatus.Active or GrantStatus.Paused))
            throw new RefundDeniedException("grant is unavailable for refund");
        if (context.Grant.NonrefundableAfterTransfer || context.Grant.Status == GrantStatus.Transferred)
            throw new RefundDeniedException("transferred grants are not refundable");
        if (context.Grant.FinalSale)
            throw new RefundDeniedException("final-sale grants are not refundable");
        if (context.HasActiveHold || context.HasUnreversedUse)
            throw new RefundDeniedException("grant has an active hold or unreversed use");
    }
}
