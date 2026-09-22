using BayAreaDanceHub.Credit.Application;
using RefundRequirement = BayAreaDanceHub.Credit.Application.RefundLineRequirement;
using Npgsql;
using BayAreaDanceHub.Credit.Domain;
using Dancehub.Common.V1;
using Dancehub.Credits.V1;
using Google.Protobuf.WellKnownTypes;
using Grpc.Core;
using ProtoCreditHold = Dancehub.Credits.V1.CreditHold;
using DomainCreditAmount = BayAreaDanceHub.Credit.Domain.CreditAmount;
using DomainCreditHoldState = BayAreaDanceHub.Credit.Domain.CreditHoldState;
using CommonCreditAmount = Dancehub.Common.V1.CreditAmount;
using ProtoRefundEligibility = Dancehub.Credits.V1.RefundEligibility;

internal sealed class CreditGrpcService(CreditApplicationService application) : CreditService.CreditServiceBase
{
    public override async Task<GetBalancesResponse> GetBalances(GetBalancesRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        var response = new GetBalancesResponse();
		response.Balances.AddRange((await application.GetBalances(identity, ct)).Select(value => new Balance { StudioId = value.StudioId?.ToString() ?? "", Available = new CommonCreditAmount { Units = value.AvailableCredits }, UnlimitedActive = value.UnlimitedActive }));
        return response;
    });

    public override async Task<ListGrantsResponse> ListGrants(ListGrantsRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        var response = new ListGrantsResponse { Page = new PageResponse() };
        response.Grants.AddRange((await application.ListGrants(identity, PageSize(request.Page), ct)).Select(ToProto));
        return response;
    });

    public override async Task<ListLedgerResponse> ListLedger(ListLedgerRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        var response = new ListLedgerResponse { Page = new PageResponse() };
		response.Entries.AddRange((await application.ListLedger(identity, PageSize(request.Page, 200), ct)).Select(value => new LedgerEntry { Id = value.Id.ToString(), StudioId = value.StudioId?.ToString() ?? "", GrantId = value.GrantId.ToString(), BookingId = value.BookingId?.ToString() ?? "", Kind = ProtoLedgerKind(value.Kind), CreditDelta = value.CreditDelta, Reason = value.Reason, CreatedAt = Timestamp.FromDateTimeOffset(value.CreatedAt) }));
        return response;
    });

	public override async Task<ProtoCreditHold> PlaceHold(PlaceHoldRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) => ToProto(await application.PlaceHold(identity, identity.UserId, new StudioId(ParseGuid(request.StudioId, "studio_id")), new BookingId(ParseGuid(request.BookingId, "booking_id")), new DomainCreditAmount(request.Amount?.Units ?? 0), request.ClassStartsAt?.ToDateTimeOffset() ?? throw new ArgumentException("class_starts_at is required"), Audit(request.Audit), ct)));
    public override Task<ProtoCreditHold> CaptureHold(CaptureHoldRequest request, ServerCallContext context) => ChangeHold(request.HoldId, request.Audit, "CAPTURE", context);
    public override Task<ProtoCreditHold> ReleaseHold(ReleaseHoldRequest request, ServerCallContext context) => ChangeHold(request.HoldId, request.Audit, "RELEASE", context);
    public override Task<ProtoCreditHold> ReverseCapture(ReverseCaptureRequest request, ServerCallContext context) => ChangeHold(request.HoldId, request.Audit, "REVERSAL", context);

    public override async Task<Grant> GrantFromOrder(GrantFromOrderRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        var entitlement = request.Entitlement ?? throw new ArgumentException("entitlement snapshot is required");
		var kind = entitlement.EntitlementCase switch { EntitlementSnapshot.EntitlementOneofCase.Credits => EntitlementKind.Credits, EntitlementSnapshot.EntitlementOneofCase.Unlimited => EntitlementKind.Unlimited, _ => throw new ArgumentException("invalid entitlement kind") };
		var creditUnits = entitlement.Credits?.Amount?.Units;
		var validityDays = entitlement.EntitlementCase == EntitlementSnapshot.EntitlementOneofCase.Credits ? entitlement.Credits?.ValidityDays ?? 0 : entitlement.Unlimited?.ValidityDays ?? 0;
		var value = new NewEntitlement(ParseGuid(request.OrderLineId.Split(':', 2)[0], "order_line_id"), request.OrderLineId, ParseGuid(request.ProductVersionId, "product_version_id"), ParseGuid(request.UserId, "user_id"), string.IsNullOrWhiteSpace(entitlement.StudioId) ? null : ParseGuid(entitlement.StudioId, "studio_id"), kind, creditUnits, validityDays, entitlement.FinalSale);
        return ToProto(await application.GrantFromOrder(identity, value, Audit(request.Audit, $"grant:{request.OrderLineId}"), ct));
    });

    public override async Task<ProtoRefundEligibility> RequestUnusedProductRefund(RequestUnusedProductRefundRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        var result = await application.RefundEligibility(identity, ParseGuid(request.UserId, "user_id"), Requirements(request.Requirements), ct);
        return new ProtoRefundEligibility { Eligible = result.Eligible, Reason = result.Reason };
    });
    public override Task<GrantOperationResponse> ReserveRefund(ReserveRefundRequest request, ServerCallContext context) => ChangeRefund(request.UserId, request.OrderId, request.AttemptId, request.Requirements, request.Audit, "RESERVE", context);
    public override Task<GrantOperationResponse> ReleaseRefund(ReleaseRefundRequest request, ServerCallContext context) => ChangeRefund(request.UserId, request.OrderId, request.AttemptId, request.Requirements, request.Audit, "RELEASE", context);
    public override Task<GrantOperationResponse> RevokeRefundedGrant(RevokeRefundedGrantRequest request, ServerCallContext context) => ChangeRefund(request.UserId, request.OrderId, request.AttemptId, request.Requirements, request.Audit, "REVOKE", context);

    public override async Task<GrantOperationResponse> TransferGrant(TransferGrantRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) => ToProto(await application.Transfer(identity, GrantId.Parse(request.GrantId), new UserId(ParseGuid(request.TargetUserId, "target_user_id")), Audit(request.Audit, requireReason: true), ct)));
    public override Task<GrantOperationResponse> PauseGrant(PauseGrantRequest request, ServerCallContext context) => ChangeAvailability(request.GrantId, request.Audit, true, context);
    public override Task<GrantOperationResponse> ResumeGrant(ResumeGrantRequest request, ServerCallContext context) => ChangeAvailability(request.GrantId, request.Audit, false, context);

    private async Task<ProtoCreditHold> ChangeHold(string holdId, AuditContext? audit, string operation, ServerCallContext context) => await Execute(context, async (identity, ct) => ToProto(await application.ChangeHold(identity, ParseGuid(holdId, "hold_id"), operation, Audit(audit), ct)));
    private async Task<GrantOperationResponse> ChangeRefund(string userId, string orderId, string attemptId, IEnumerable<Dancehub.Credits.V1.RefundLineRequirement> lines, AuditContext? audit, string operation, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        RequireService(identity, "orderservice");
        return ToProto(await application.ChangeRefund(identity, ParseGuid(userId, "user_id"), ParseGuid(attemptId, "attempt_id"), ParseGuid(orderId, "order_id"), Requirements(lines), operation, Audit(audit), ct));
    });
    public override async Task<GrantOperationResponse> CompensateBookingHold(CompensateBookingHoldRequest request, ServerCallContext context) => await Execute(context, async (identity, ct) =>
    {
        RequireService(identity, "scheduling-worker");
        return ToProto(await application.CompensateBooking(identity, ParseGuid(request.UserId, "user_id"), ParseGuid(request.StudioId, "studio_id"), ParseGuid(request.BookingId, "booking_id"), Audit(request.Audit), ct));
    });
    private static void RequireService(RequestIdentity identity, string principal)
    {
        if (identity.ActorKind != "service" || identity.ServicePrincipal != principal) throw new UnauthorizedAccessException(principal + " service principal required");
    }
    private static IReadOnlyList<RefundRequirement> Requirements(IEnumerable<Dancehub.Credits.V1.RefundLineRequirement> lines) => lines.Select(l => new RefundRequirement(ParseGuid(l.OrderLineId, "order_line_id"), l.Quantity)).ToArray();
    private async Task<GrantOperationResponse> ChangeAvailability(string grantId, AuditContext? audit, bool pause, ServerCallContext context) => await Execute(context, async (identity, ct) => ToProto(await application.ChangeAvailability(identity, GrantId.Parse(grantId), pause, Audit(audit, requireReason: true), ct)));

    private static RequestIdentity Identity(ServerCallContext context)
    {
        var tenantRoles = Metadata(context, "x-tenant-roles").Split(',', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries).ToHashSet(StringComparer.Ordinal);
		var globalRoles = Metadata(context, "x-global-roles").Split(',', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries).ToHashSet(StringComparer.Ordinal);
        var requestId = Metadata(context, "x-request-id");
        if (string.IsNullOrWhiteSpace(requestId)) throw new RpcException(new Status(StatusCode.Unauthenticated, "request identity is incomplete"));
        return new(Metadata(context, "x-actor-kind") == "service" ? Guid.Empty : ParseGuid(Metadata(context, "x-user-id"), "authenticated user"), Guid.TryParse(Metadata(context, "x-studio-id"), out var studio) ? studio : null, tenantRoles, requestId, globalRoles, Metadata(context, "x-actor-kind"), Metadata(context, "x-service-principal"));
    }

    private static string Metadata(ServerCallContext context, string key) =>
        context.RequestHeaders.FirstOrDefault(header => header.Key.Equals(key, StringComparison.OrdinalIgnoreCase))?.Value ?? "";

    private static AuditData Audit(AuditContext? audit, string? fallbackKey = null, bool requireReason = false)
    {
        var key = string.IsNullOrWhiteSpace(audit?.IdempotencyKey) ? fallbackKey : audit.IdempotencyKey;
        if (string.IsNullOrWhiteSpace(key)) throw new ArgumentException("idempotency_key is required");
        var reason = audit?.Reason?.Trim() ?? "";
        if (requireReason && reason.Length == 0) throw new ArgumentException("reason is required");
        return new(new(key), reason);
    }

    private static async Task<T> Execute<T>(ServerCallContext context, Func<RequestIdentity, CancellationToken, Task<T>> action)
    {
        for (var attempt = 0; ; attempt++)
        try { return await action(Identity(context), context.CancellationToken); }
        catch (PostgresException error) when (error.SqlState is "40001" or "40P01" && attempt < 3)
        { await Task.Delay(TimeSpan.FromMilliseconds(25 * (1 << attempt)), context.CancellationToken); }
        catch (RpcException) { throw; }
        catch (ArgumentException error) { throw new RpcException(new(StatusCode.InvalidArgument, error.Message)); }
        catch (UnauthorizedAccessException error) { throw new RpcException(new(StatusCode.PermissionDenied, error.Message)); }
        catch (KeyNotFoundException error) { throw new RpcException(new(StatusCode.NotFound, error.Message)); }
        catch (InsufficientCreditException error) { throw new RpcException(new(StatusCode.FailedPrecondition, error.Message)); }
        catch (InvalidGrantTransitionException error) { throw new RpcException(new(StatusCode.FailedPrecondition, error.Message)); }
        catch (RefundDeniedException error) { throw new RpcException(new(StatusCode.FailedPrecondition, error.Message)); }
    }

    private static Grant ToProto(GrantSnapshot value)
    {
		var result = new Grant { Id = value.Id.ToString(), UserId = value.UserId.Value.ToString(), StudioId = value.StudioId?.ToString() ?? "", Remaining = new CommonCreditAmount { Units = value.RemainingCredits ?? 0 }, Unlimited = value.Kind == EntitlementKind.Unlimited, Status = ProtoGrantStatus(value.Status), RemainingValiditySeconds = value.RemainingValiditySeconds ?? 0, FinalSale = value.FinalSale };
        if (value.Validity.ExpiresAt is not null) result.ExpiresAt = Timestamp.FromDateTimeOffset(value.Validity.ExpiresAt.Value);
        if (value.PausedAt is not null) result.PausedAt = Timestamp.FromDateTimeOffset(value.PausedAt.Value);
        return result;
    }
	private static ProtoCreditHold ToProto(HoldView value) => new() { Id = value.Id.ToString(), BookingId = value.BookingId.Value.ToString(), Amount = new CommonCreditAmount { Units = value.Amount.Units }, Status = value.Status switch { DomainCreditHoldState.Active => CreditHoldStatus.Active, DomainCreditHoldState.Captured => CreditHoldStatus.Captured, DomainCreditHoldState.Released => CreditHoldStatus.Released, DomainCreditHoldState.Reversed => CreditHoldStatus.Reversed, _ => CreditHoldStatus.Unspecified } };
	private static Dancehub.Credits.V1.GrantStatus ProtoGrantStatus(BayAreaDanceHub.Credit.Domain.GrantStatus value) => System.Enum.Parse<Dancehub.Credits.V1.GrantStatus>(value.ToString(), true);
	private static LedgerKind ProtoLedgerKind(string value) => System.Enum.TryParse<LedgerKind>(string.Concat(value.Split('_').Select(part => char.ToUpperInvariant(part[0]) + part[1..].ToLowerInvariant())), out var result) ? result : LedgerKind.Unspecified;
    private static GrantOperationResponse ToProto(OperationView value) { var result = new GrantOperationResponse { Operation = value.Operation, GrantId = value.GrantId.Value == Guid.Empty ? "" : value.GrantId.ToString(), SuccessorGrantId = value.SuccessorGrantId?.ToString() ?? "", Status = value.Status }; if (value.ExpiresAt is not null) result.ExpiresAt = Timestamp.FromDateTimeOffset(value.ExpiresAt.Value); return result; }
    private static Guid ParseGuid(string value, string field) => Guid.TryParse(value, out var id) ? id : throw new ArgumentException($"{field} must be a UUID");
    private static IReadOnlyList<Guid> Lines(IEnumerable<string> values) { var result = values.Select(value => ParseGuid(value, "order_line_id")).ToArray(); if (result.Length == 0) throw new ArgumentException("order_line_ids are required"); return result; }
    private static int PageSize(PageRequest? page, int fallback = 24) => page is null || page.PageSize < 1 || page.PageSize > 200 ? fallback : page.PageSize;
}
