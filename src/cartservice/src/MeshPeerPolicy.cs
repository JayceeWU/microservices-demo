using Grpc.Core;
using Grpc.Core.Interceptors;

// Binds asserted identity metadata to the workload identity the service mesh proved during
// the mTLS handshake. Mirrors src/dancehub/internal/platform/mesh.go: the peer named by the
// SPIFFE URI in x-forwarded-client-cert must be an allowed caller, and a `service` actor may
// only claim a principal bound to that peer's ServiceAccount. Enforcement is off unless
// MESH_PEER_ENFORCEMENT=true, so Compose and the Kustomize development overlays keep working.
//
// The credit service carries an identical copy (src/creditservice/MeshPeerPolicy.cs): the two
// projects are built from separate Docker contexts and share no class library.
public sealed class MeshPeerPolicy
{
    public bool Enforce { get; }
    public string TrustDomain { get; }
    public string Namespace { get; }
    private readonly IReadOnlyDictionary<string, IReadOnlySet<string>> peers;

    private MeshPeerPolicy(bool enforce, string trustDomain, string ns, IReadOnlyDictionary<string, IReadOnlySet<string>> peers)
    {
        Enforce = enforce;
        TrustDomain = trustDomain;
        Namespace = ns;
        this.peers = peers;
    }

    public static MeshPeerPolicy Disabled { get; } = new(false, "cluster.local", "", new Dictionary<string, IReadOnlySet<string>>());

    public static MeshPeerPolicy FromEnvironment(Func<string, string?> environment)
    {
        if (!string.Equals((environment("MESH_PEER_ENFORCEMENT") ?? "").Trim(), "true", StringComparison.OrdinalIgnoreCase))
        {
            return Disabled;
        }
        var ns = (environment("MESH_NAMESPACE") ?? "").Trim();
        if (ns.Length == 0)
        {
            throw new InvalidOperationException("MESH_NAMESPACE is required when MESH_PEER_ENFORCEMENT is true");
        }
        var peers = ParsePeers(environment("MESH_ALLOWED_PEERS"));
        if (peers.Count == 0)
        {
            throw new InvalidOperationException("MESH_ALLOWED_PEERS must list at least one caller when MESH_PEER_ENFORCEMENT is true");
        }
        var trustDomain = (environment("MESH_TRUST_DOMAIN") ?? "").Trim();
        return new MeshPeerPolicy(true, trustDomain.Length == 0 ? "cluster.local" : trustDomain, ns, peers);
    }

    // "sa,sa=principal|principal" -> service account => principals that workload may assert.
    public static IReadOnlyDictionary<string, IReadOnlySet<string>> ParsePeers(string? value)
    {
        var peers = new Dictionary<string, IReadOnlySet<string>>(StringComparer.Ordinal);
        foreach (var rawEntry in (value ?? "").Split(','))
        {
            var entry = rawEntry.Trim();
            if (entry.Length == 0) continue;
            var separator = entry.IndexOf('=');
            var account = (separator < 0 ? entry : entry[..separator]).Trim();
            if (account.Length == 0)
            {
                throw new InvalidOperationException($"MESH_ALLOWED_PEERS entry \"{entry}\" has no service account");
            }
            var principals = separator < 0
                ? []
                : entry[(separator + 1)..].Split('|', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries);
            peers[account] = principals.ToHashSet(StringComparer.Ordinal);
        }
        return peers;
    }

    // Envoy appends the current client certificate as the last x-forwarded-client-cert element,
    // so only the last element is trustworthy.
    public static (string TrustDomain, string Namespace, string ServiceAccount)? PeerServiceAccount(string? header)
    {
        var value = (header ?? "").Trim();
        if (value.Length == 0) return null;
        var elements = value.Split(',');
        foreach (var pair in elements[^1].Split(';'))
        {
            var separator = pair.IndexOf('=');
            if (separator < 0 || !pair[..separator].Trim().Equals("URI", StringComparison.OrdinalIgnoreCase)) continue;
            var uri = pair[(separator + 1)..].Trim().Trim('"');
            if (!uri.StartsWith("spiffe://", StringComparison.Ordinal)) return null;
            // <trust-domain>/ns/<namespace>/sa/<service-account>
            var parts = uri["spiffe://".Length..].Split('/');
            if (parts.Length != 5 || parts[1] != "ns" || parts[3] != "sa" || parts[0].Length == 0 || parts[2].Length == 0 || parts[4].Length == 0)
            {
                return null;
            }
            return (parts[0], parts[2], parts[4]);
        }
        return null;
    }

    // Returns null when the call is allowed, otherwise the status to fail it with.
    public Status? Authorize(string? xfcc, string? actorKind, string? servicePrincipal)
    {
        if (!Enforce) return null;
        var peer = PeerServiceAccount(xfcc);
        if (peer is null)
        {
            return new Status(StatusCode.Unauthenticated, "mesh peer identity is required");
        }
        if (peer.Value.TrustDomain != TrustDomain || peer.Value.Namespace != Namespace)
        {
            return new Status(StatusCode.PermissionDenied, "peer belongs to another trust domain or namespace");
        }
        if (!peers.TryGetValue(peer.Value.ServiceAccount, out var principals))
        {
            return new Status(StatusCode.PermissionDenied, "peer workload is not an allowed caller");
        }
        if (actorKind == "service" && !principals.Contains(servicePrincipal ?? ""))
        {
            return new Status(StatusCode.Unauthenticated, "service principal is not bound to the peer workload");
        }
        return null;
    }
}

// Runs the peer check before any business handler; the gRPC health service stays open so
// probes and the gateway's dependency check are not tied to caller identity.
internal sealed class MeshPeerInterceptor(MeshPeerPolicy policy) : Interceptor
{
    private const string HealthServicePrefix = "/grpc.health.v1.Health/";

    public override Task<TResponse> UnaryServerHandler<TRequest, TResponse>(TRequest request, ServerCallContext context, UnaryServerMethod<TRequest, TResponse> continuation)
    {
        Enforce(context);
        return continuation(request, context);
    }

    public override Task ServerStreamingServerHandler<TRequest, TResponse>(TRequest request, IServerStreamWriter<TResponse> responseStream, ServerCallContext context, ServerStreamingServerMethod<TRequest, TResponse> continuation)
    {
        Enforce(context);
        return continuation(request, responseStream, context);
    }

    public override Task<TResponse> ClientStreamingServerHandler<TRequest, TResponse>(IAsyncStreamReader<TRequest> requestStream, ServerCallContext context, ClientStreamingServerMethod<TRequest, TResponse> continuation)
    {
        Enforce(context);
        return continuation(requestStream, context);
    }

    public override Task DuplexStreamingServerHandler<TRequest, TResponse>(IAsyncStreamReader<TRequest> requestStream, IServerStreamWriter<TResponse> responseStream, ServerCallContext context, DuplexStreamingServerMethod<TRequest, TResponse> continuation)
    {
        Enforce(context);
        return continuation(requestStream, responseStream, context);
    }

    private void Enforce(ServerCallContext context)
    {
        if (context.Method.StartsWith(HealthServicePrefix, StringComparison.Ordinal)) return;
        var denied = policy.Authorize(Header(context, "x-forwarded-client-cert"), Header(context, "x-actor-kind"), Header(context, "x-service-principal"));
        if (denied is { } status)
        {
            throw new RpcException(status);
        }
    }

    private static string Header(ServerCallContext context, string key) =>
        context.RequestHeaders.FirstOrDefault(header => header.Key.Equals(key, StringComparison.OrdinalIgnoreCase))?.Value ?? "";
}
