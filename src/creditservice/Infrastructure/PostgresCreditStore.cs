using System.Data;
using BayAreaDanceHub.Credit.Application;
using BayAreaDanceHub.Credit.Domain;
using Npgsql;

namespace BayAreaDanceHub.Credit.Infrastructure;

public sealed partial class PostgresCreditStore : ICreditStore
{
    private readonly NpgsqlDataSource _dataSource;

    public PostgresCreditStore(NpgsqlDataSource dataSource) => _dataSource = dataSource;
    private const string GrantSelect = """
        SELECT id,user_id,studio_id,source_order_line_id,product_version_id,kind::text,
               granted_credits,remaining_credits,valid_from,expires_at,status,paused_at,
               remaining_validity_seconds,final_sale,nonrefundable_after_transfer,refund_pending_at,created_at
        FROM credit.grants
        """;

    public async Task<IReadOnlyList<BalanceView>> GetBalances(RequestIdentity identity, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct);
        await using var command = Command(session, """
            SELECT studio_id, COALESCE(sum(remaining_credits) FILTER (WHERE kind='CREDITS'),0)::int,
                   bool_or(kind='UNLIMITED' AND revoked_at IS NULL AND valid_from<=now() AND (expires_at IS NULL OR expires_at>now()))
            FROM credit.grants WHERE user_id=$1 AND status='ACTIVE' AND revoked_at IS NULL AND refund_pending_at IS NULL
              AND valid_from<=now() AND (expires_at IS NULL OR expires_at>now())
            GROUP BY studio_id ORDER BY studio_id NULLS FIRST
            """, identity.UserId);
        var result = new List<BalanceView>();
        await using var reader = await command.ExecuteReaderAsync(ct);
        while (await reader.ReadAsync(ct))
            result.Add(new(reader.IsDBNull(0) ? null : reader.GetGuid(0), reader.GetInt32(1), !reader.IsDBNull(2) && reader.GetBoolean(2)));
        await reader.DisposeAsync();
        await session.CommitAsync(ct);
        return result;
    }

    public async Task<IReadOnlyList<GrantSnapshot>> ListGrants(RequestIdentity identity, int limit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct);
        await using var command = Command(session, GrantSelect + " WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2", identity.UserId, limit);
        var result = new List<GrantSnapshot>();
        await using var reader = await command.ExecuteReaderAsync(ct);
        while (await reader.ReadAsync(ct)) result.Add(ReadGrant(reader));
        await reader.DisposeAsync();
        await session.CommitAsync(ct);
        return result;
    }

    public async Task<IReadOnlyList<LedgerView>> ListLedger(RequestIdentity identity, int limit, CancellationToken ct)
    {
        await using var session = await TenantDbSession.OpenAsync(_dataSource, identity, ct);
        await using var command = Command(session, "SELECT id,studio_id,grant_id,booking_id,kind::text,credit_delta,reason,created_at FROM credit.ledger_entries WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2", identity.UserId, limit);
        var result = new List<LedgerView>();
        await using var reader = await command.ExecuteReaderAsync(ct);
        while (await reader.ReadAsync(ct))
            result.Add(new(reader.GetGuid(0), reader.IsDBNull(1) ? null : reader.GetGuid(1), new(reader.GetGuid(2)), reader.IsDBNull(3) ? null : reader.GetGuid(3), reader.GetString(4), reader.GetInt32(5), reader.GetString(6), reader.GetFieldValue<DateTimeOffset>(7)));
        await reader.DisposeAsync();
        await session.CommitAsync(ct);
        return result;
    }

    private static GrantSnapshot ReadGrant(NpgsqlDataReader reader) => new(
        new(reader.GetGuid(0)), new(reader.GetGuid(1)), reader.IsDBNull(2) ? null : new StudioId(reader.GetGuid(2)),
        reader.GetGuid(3), reader.GetGuid(4), reader.GetString(5) == "UNLIMITED" ? EntitlementKind.Unlimited : EntitlementKind.Credits,
        reader.IsDBNull(6) ? null : reader.GetInt32(6), reader.IsDBNull(7) ? null : reader.GetInt32(7),
        new(reader.GetFieldValue<DateTimeOffset>(8), reader.IsDBNull(9) ? null : reader.GetFieldValue<DateTimeOffset>(9)),
        Enum.Parse<GrantStatus>(reader.GetString(10), true), reader.IsDBNull(11) ? null : reader.GetFieldValue<DateTimeOffset>(11),
        reader.IsDBNull(12) ? null : reader.GetInt64(12), reader.GetBoolean(13), reader.GetBoolean(14),
        reader.IsDBNull(15) ? null : reader.GetFieldValue<DateTimeOffset>(15));

    private static async Task<GrantSnapshot> LoadGrant(TenantDbSession session, Guid id, bool locked, CancellationToken ct)
    {
        await using var command = Command(session, GrantSelect + " WHERE id=$1" + (locked ? " FOR UPDATE" : ""), id);
        await using var reader = await command.ExecuteReaderAsync(ct);
        if (!await reader.ReadAsync(ct)) throw new KeyNotFoundException("grant not found");
        return ReadGrant(reader);
    }

    private static NpgsqlCommand Command(TenantDbSession session, string sql, params object[] values)
    {
        var command = new NpgsqlCommand(sql, session.Connection, session.Transaction);
        for (var index = 0; index < values.Length; index++) command.Parameters.AddWithValue(values[index]);
        return command;
    }

    private static async Task Execute(TenantDbSession session, string sql, CancellationToken ct, params object[] values)
    {
        await using var command = Command(session, sql, values);
        await command.ExecuteNonQueryAsync(ct);
    }

    private static async Task InsertLedger(TenantDbSession session, Guid user, Guid? studio, Guid grant, Guid? hold, Guid? booking, string kind, int delta, string key, string reason, Guid? actor, CancellationToken ct) =>
        await Execute(session, "INSERT INTO credit.ledger_entries(user_id,studio_id,grant_id,hold_id,booking_id,kind,credit_delta,idempotency_key,reason,actor_id) VALUES($1,$2,$3,$4,$5,$6::credit.ledger_kind,$7,$8,$9,$10) ON CONFLICT(idempotency_key) DO NOTHING", ct, user, (object?)studio ?? DBNull.Value, grant, (object?)hold ?? DBNull.Value, (object?)booking ?? DBNull.Value, kind, delta, key, reason, (object?)actor ?? DBNull.Value);

    private static async Task InsertOperation(TenantDbSession session, Guid grant, Guid? successor, string operation, Guid source, Guid? target, Guid? studio, Guid actor, string reason, string before, string after, string key, string requestId, CancellationToken ct) =>
        await Execute(session, "INSERT INTO credit.grant_operations(grant_id,successor_grant_id,operation,source_user_id,target_user_id,studio_id,actor_id,reason,before_state,after_state,idempotency_key,request_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,jsonb_build_object('status',$9),jsonb_build_object('status',$10),$11,$12) ON CONFLICT(idempotency_key) DO NOTHING", ct, grant, (object?)successor ?? DBNull.Value, operation, source, (object?)target ?? DBNull.Value, (object?)studio ?? DBNull.Value, actor, reason, before, after, key, requestId);

    private static string KindText(EntitlementKind kind) => kind == EntitlementKind.Unlimited ? "UNLIMITED" : "CREDITS";
    private static string StatusText(GrantStatus status) => status.ToString().ToUpperInvariant();
    private static string DefaultReason(AuditData audit, string fallback) => string.IsNullOrWhiteSpace(audit.Reason) ? fallback : audit.Reason;
    private static void EnsureStudio(RequestIdentity identity, StudioId? studio)
    {
        if (!identity.IsPlatformAdmin && studio?.Value != identity.StudioId) throw new UnauthorizedAccessException("grant is outside selected studio");
    }
}
