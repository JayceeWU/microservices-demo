using Grpc.Core;
using Xunit;

namespace BayAreaDanceHub.Credit.Tests;

public sealed class MeshPeerPolicyTests
{
    private const string GatewayXfcc =
        "By=spiffe://cluster.local/ns/dancehub/sa/creditservice;Hash=7d3f;Subject=\"\";URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice";

    private static MeshPeerPolicy Policy() => MeshPeerPolicy.FromEnvironment(name => name switch
    {
        "MESH_PEER_ENFORCEMENT" => "true",
        "MESH_NAMESPACE" => "dancehub",
        "MESH_ALLOWED_PEERS" => "gatewayservice, orderservice=orderservice|order-refund-worker",
        _ => null,
    });

    [Fact]
    public void Peer_identity_comes_from_the_last_certificate_element()
    {
        const string forged = "URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice," +
            "By=spiffe://cluster.local/ns/dancehub/sa/creditservice;Hash=1a2b;URI=spiffe://cluster.local/ns/dancehub/sa/orderservice";
        Assert.Equal(("cluster.local", "dancehub", "orderservice"), MeshPeerPolicy.PeerServiceAccount(forged));
        foreach (var header in new[] { "", "Hash=1a2b", "URI=https://example.com", "URI=spiffe://cluster.local/ns/x/deployment/y" })
        {
            Assert.Null(MeshPeerPolicy.PeerServiceAccount(header));
        }
    }

    [Theory]
    [InlineData(GatewayXfcc, "human", "", null)]
    [InlineData(GatewayXfcc, "", "", null)]
    [InlineData("URI=spiffe://cluster.local/ns/dancehub/sa/orderservice", "service", "orderservice", null)]
    [InlineData("URI=spiffe://cluster.local/ns/dancehub/sa/orderservice", "service", "order-refund-worker", null)]
    [InlineData("URI=spiffe://cluster.local/ns/dancehub/sa/orderservice", "human", "", null)]
    [InlineData("", "human", "", StatusCode.Unauthenticated)]
    [InlineData(GatewayXfcc, "service", "orderservice", StatusCode.Unauthenticated)]
    [InlineData("URI=spiffe://cluster.local/ns/dancehub/sa/chatservice", "human", "", StatusCode.PermissionDenied)]
    [InlineData("URI=spiffe://cluster.local/ns/staging/sa/gatewayservice", "human", "", StatusCode.PermissionDenied)]
    [InlineData("URI=spiffe://evil.example/ns/dancehub/sa/gatewayservice", "human", "", StatusCode.PermissionDenied)]
    public void Service_principals_are_bound_to_the_peer_workload(string xfcc, string actorKind, string principal, StatusCode? expected)
    {
        Assert.Equal(expected, Policy().Authorize(xfcc, actorKind, principal)?.StatusCode);
    }

    [Fact]
    public void Inactive_unless_enforcement_is_enabled()
    {
        var policy = MeshPeerPolicy.FromEnvironment(_ => null);
        Assert.False(policy.Enforce);
        Assert.Null(policy.Authorize("", "service", "anything"));
    }

    [Fact]
    public void Configuration_errors_fail_fast()
    {
        Assert.Throws<InvalidOperationException>(() => MeshPeerPolicy.FromEnvironment(name => name == "MESH_PEER_ENFORCEMENT" ? "true" : name == "MESH_NAMESPACE" ? "dancehub" : null));
        Assert.Throws<InvalidOperationException>(() => MeshPeerPolicy.FromEnvironment(name => name == "MESH_PEER_ENFORCEMENT" ? "true" : name == "MESH_ALLOWED_PEERS" ? "gatewayservice" : null));
        Assert.Throws<InvalidOperationException>(() => MeshPeerPolicy.ParsePeers("gatewayservice,=worker"));
        Assert.Equal("cluster.local", Policy().TrustDomain);
    }
}
