using StackExchange.Redis;

/// <summary>
/// Key/value storage for serialized carts with compare-and-set semantics. The gRPC service
/// applies every change as read → mutate → CompareAndSet so that two concurrent requests for
/// the same cart cannot silently overwrite each other's update.
/// </summary>
public interface ICartStore
{
    /// <summary>Returns the stored value or null when the cart does not exist.</summary>
    Task<string?> Get(string key, CancellationToken token);

    /// <summary>
    /// Stores <paramref name="next"/> only while the key still holds <paramref name="expected"/>
    /// (null meaning "does not exist"). Returns false when another writer changed it first.
    /// </summary>
    Task<bool> CompareAndSet(string key, string? expected, string next, CancellationToken token);

    Task Remove(string key, CancellationToken token);
}

/// <summary>
/// Redis implementation. Carts are plain string keys with a 7-day sliding expiry: reads refresh
/// the TTL and writes reset it. CompareAndSet runs as a MULTI/EXEC transaction guarded by a
/// WATCH-style condition on the previous value, which Redis evaluates atomically.
/// </summary>
public sealed class RedisCartStore(IConnectionMultiplexer redis) : ICartStore
{
    public static readonly TimeSpan SlidingExpiration = TimeSpan.FromDays(7);

    public async Task<string?> Get(string key, CancellationToken token)
    {
        token.ThrowIfCancellationRequested();
        var value = await redis.GetDatabase().StringGetSetExpiryAsync(key, SlidingExpiration);
        return value.IsNull ? null : (string?)value;
    }

    public async Task<bool> CompareAndSet(string key, string? expected, string next, CancellationToken token)
    {
        token.ThrowIfCancellationRequested();
        var transaction = redis.GetDatabase().CreateTransaction();
        transaction.AddCondition(expected is null ? Condition.KeyNotExists(key) : Condition.StringEqual(key, expected));
        _ = transaction.StringSetAsync(key, next, SlidingExpiration);
        return await transaction.ExecuteAsync();
    }

    public Task Remove(string key, CancellationToken token)
    {
        token.ThrowIfCancellationRequested();
        return redis.GetDatabase().KeyDeleteAsync(key);
    }
}
