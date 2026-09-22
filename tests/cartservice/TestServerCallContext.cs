using Grpc.Core;

namespace BayAreaDanceHub.Cart.Tests;

internal sealed class TestServerCallContext : ServerCallContext
{
    private readonly Metadata _requestHeaders;

    private TestServerCallContext(Metadata requestHeaders, CancellationToken cancellationToken)
    {
        _requestHeaders = requestHeaders;
        CancellationTokenCore = cancellationToken;
    }

    public static TestServerCallContext Create(string? userId = null, CancellationToken cancellationToken = default)
    {
        var headers = new Metadata();
        if (userId is not null) headers.Add("x-user-id", userId);
        return new TestServerCallContext(headers, cancellationToken);
    }

    protected override string MethodCore => "test-method";
    protected override string HostCore => "localhost";
    protected override string PeerCore => "test-peer";
    protected override DateTime DeadlineCore => DateTime.UtcNow.AddMinutes(1);
    protected override Metadata RequestHeadersCore => _requestHeaders;
    protected override CancellationToken CancellationTokenCore { get; }
    protected override Metadata ResponseTrailersCore { get; } = new Metadata();
    protected override Status StatusCore { get; set; }
    protected override WriteOptions? WriteOptionsCore { get; set; }
    protected override AuthContext AuthContextCore { get; } = new AuthContext(string.Empty, new Dictionary<string, List<AuthProperty>>());

    protected override Task WriteResponseHeadersAsyncCore(Metadata responseHeaders) => Task.CompletedTask;

    protected override ContextPropagationToken CreatePropagationTokenCore(ContextPropagationOptions? options) =>
        throw new NotSupportedException("propagation tokens are not needed for unit tests");
}
