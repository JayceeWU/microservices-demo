using Npgsql;
using System.Data;
using BayAreaDanceHub.Credit.Application;

internal sealed class TenantDbSession : IAsyncDisposable
{
    public NpgsqlConnection Connection { get; }
    public NpgsqlTransaction Transaction { get; }

    private TenantDbSession(NpgsqlConnection connection, NpgsqlTransaction transaction)
    {
        Connection = connection;
        Transaction = transaction;
    }

    public static async Task<TenantDbSession> OpenAsync(
        NpgsqlDataSource dataSource,
        RequestIdentity identity,
        CancellationToken cancellationToken,
        IsolationLevel isolation = IsolationLevel.ReadCommitted,
        Guid? userOverride = null,
        Guid? studioOverride = null)
    {
        var connection = await dataSource.OpenConnectionAsync(cancellationToken);
        var transaction = await connection.BeginTransactionAsync(isolation, cancellationToken);
        await ConfigureAsync(
            connection,
            transaction,
            userOverride?.ToString() ?? identity.UserId.ToString(),
            studioOverride?.ToString() ?? identity.StudioId?.ToString() ?? "",
            string.Join(',', identity.TenantRoles),
            string.Join(',', identity.GlobalRoles),
            identity.ActorKind,
            identity.ServicePrincipal,
            identity.RequestId,
            cancellationToken);
        return new TenantDbSession(connection, transaction);
    }

    private static async Task ConfigureAsync(
        NpgsqlConnection connection,
        NpgsqlTransaction transaction,
        string userId,
        string studioId,
        string tenantRoles,
        string globalRoles,
        string actorKind,
        string servicePrincipal,
        string requestId,
        CancellationToken cancellationToken)
    {
        await using var command = new NpgsqlCommand("""
            SELECT set_config('app.user_id',$1,true),
                   set_config('app.studio_id',$2,true),
                   set_config('app.tenant_roles',$3,true),
                   set_config('app.global_roles',$4,true),
                   set_config('app.actor_kind',$5,true),
                   set_config('app.service_principal',$6,true),
                   set_config('app.service','creditservice',true),
                   set_config('app.request_id',$7,true)
            """, connection, transaction);
        command.Parameters.AddWithValue(userId);
        command.Parameters.AddWithValue(studioId);
        command.Parameters.AddWithValue(tenantRoles);
        command.Parameters.AddWithValue(globalRoles);
        command.Parameters.AddWithValue(actorKind);
        command.Parameters.AddWithValue(servicePrincipal);
        command.Parameters.AddWithValue(requestId);
        await command.ExecuteNonQueryAsync(cancellationToken);
    }

    public async Task CommitAsync(CancellationToken cancellationToken) => await Transaction.CommitAsync(cancellationToken);

    public async ValueTask DisposeAsync()
    {
        await Transaction.DisposeAsync();
        await Connection.DisposeAsync();
    }
}
