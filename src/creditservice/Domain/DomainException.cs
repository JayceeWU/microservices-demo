namespace BayAreaDanceHub.Credit.Domain;

public abstract class DomainException(string message) : Exception(message);
public sealed class InvalidGrantTransitionException(string message) : DomainException(message);
public sealed class InsufficientCreditException(string message) : DomainException(message);
public sealed class RefundDeniedException(string message) : DomainException(message);
