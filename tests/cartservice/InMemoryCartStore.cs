namespace BayAreaDanceHub.Cart.Tests;

/// <summary>
/// Test double with the same compare-and-set contract as the Redis store. Tests can hook
/// <see cref="BeforeCompareAndSet"/> to interleave a competing write between a request's
/// read and its write, which is exactly the window a lost update needs.
/// </summary>
internal sealed class InMemoryCartStore : ICartStore
{
    private readonly Dictionary<string, string> _values = new();
    private readonly object _gate = new();

    public Func<string, Task>? BeforeCompareAndSet { get; set; }
    public int CompareAndSetCalls { get; private set; }
    public int RejectedWrites { get; private set; }

    public Task<string?> Get(string key, CancellationToken token)
    {
        lock (_gate) return Task.FromResult(_values.TryGetValue(key, out var value) ? value : null);
    }

    public async Task<bool> CompareAndSet(string key, string? expected, string next, CancellationToken token)
    {
        if (BeforeCompareAndSet is not null) await BeforeCompareAndSet(key);
        lock (_gate)
        {
            CompareAndSetCalls++;
            var current = _values.TryGetValue(key, out var value) ? value : null;
            if (current != expected)
            {
                RejectedWrites++;
                return false;
            }
            _values[key] = next;
            return true;
        }
    }

    public Task Remove(string key, CancellationToken token)
    {
        lock (_gate) _values.Remove(key);
        return Task.CompletedTask;
    }
}
