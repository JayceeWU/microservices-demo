package platform

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const gatewayXFCC = `By=spiffe://cluster.local/ns/dancehub/sa/creditservice;Hash=7d3f;Subject="";URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice`

func creditServiceMeshPolicy(t *testing.T) MeshPolicy {
	t.Helper()
	peers, err := ParseMeshPeers("gatewayservice, schedulingservice=scheduling-worker, orderservice=orderservice|order-refund-worker")
	if err != nil {
		t.Fatal(err)
	}
	return MeshPolicy{Enforce: true, TrustDomain: "cluster.local", Namespace: "dancehub", peers: peers}
}

func TestPeerServiceAccountParsesTheLastCertificate(t *testing.T) {
	forged := `By=spiffe://cluster.local/ns/dancehub/sa/creditservice;URI=spiffe://cluster.local/ns/dancehub/sa/gatewayservice,` +
		`By=spiffe://cluster.local/ns/dancehub/sa/creditservice;Hash=1a2b;URI=spiffe://cluster.local/ns/dancehub/sa/orderservice`
	trustDomain, namespace, account, ok := PeerServiceAccount(forged)
	if !ok || trustDomain != "cluster.local" || namespace != "dancehub" || account != "orderservice" {
		t.Fatalf("parsed %q %q %q ok=%v, want the last element's orderservice identity", trustDomain, namespace, account, ok)
	}
	for _, header := range []string{"", "Hash=1a2b;Subject=\"\"", "URI=https://example.com", "URI=spiffe://cluster.local/ns/dancehub/deployment/x"} {
		if _, _, _, ok := PeerServiceAccount(header); ok {
			t.Fatalf("header %q should not yield a SPIFFE workload identity", header)
		}
	}
}

func TestMeshPolicyAuthorize(t *testing.T) {
	policy := creditServiceMeshPolicy(t)
	cases := []struct {
		name, xfcc, actorKind, principal string
		want                             codes.Code
	}{
		{"gateway forwards a human", gatewayXFCC, "human", "", codes.OK},
		{"gateway forwards an anonymous query", gatewayXFCC, "", "", codes.OK},
		{"scheduling worker asserts its own principal", `URI=spiffe://cluster.local/ns/dancehub/sa/schedulingservice`, "service", "scheduling-worker", codes.OK},
		{"order service asserts one of its principals", `URI=spiffe://cluster.local/ns/dancehub/sa/orderservice`, "service", "order-refund-worker", codes.OK},
		{"order service forwards a human", `URI=spiffe://cluster.local/ns/dancehub/sa/orderservice`, "human", "", codes.OK},
		{"missing mesh identity", "", "human", "", codes.Unauthenticated},
		{"gateway cannot pose as the scheduling worker", gatewayXFCC, "service", "scheduling-worker", codes.Unauthenticated},
		{"scheduling cannot pose as the order worker", `URI=spiffe://cluster.local/ns/dancehub/sa/schedulingservice`, "service", "orderservice", codes.Unauthenticated},
		{"unlisted workload", `URI=spiffe://cluster.local/ns/dancehub/sa/chatservice`, "human", "", codes.PermissionDenied},
		{"other namespace", `URI=spiffe://cluster.local/ns/staging/sa/gatewayservice`, "human", "", codes.PermissionDenied},
		{"other trust domain", `URI=spiffe://evil.example/ns/dancehub/sa/gatewayservice`, "human", "", codes.PermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := status.Code(policy.Authorize(tc.xfcc, tc.actorKind, tc.principal)); got != tc.want {
				t.Fatalf("code = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMeshPolicyIsInactiveUnlessEnforced(t *testing.T) {
	var inactive MeshPolicy
	if err := inactive.Authorize("", "service", "anything"); err != nil {
		t.Fatalf("zero policy must not enforce: %v", err)
	}
}

func TestParseMeshPeersRejectsEmptyAccount(t *testing.T) {
	if _, err := ParseMeshPeers("gatewayservice,=scheduling-worker"); err == nil {
		t.Fatal("entry without a service account must be rejected")
	}
}

func TestMeshPolicyFromEnvRequiresPeersWhenEnforcing(t *testing.T) {
	t.Setenv("MESH_PEER_ENFORCEMENT", "true")
	t.Setenv("MESH_NAMESPACE", "dancehub")
	t.Setenv("MESH_ALLOWED_PEERS", "")
	if _, err := MeshPolicyFromEnv(); err == nil {
		t.Fatal("enforcing without callers must fail fast")
	}
	t.Setenv("MESH_ALLOWED_PEERS", "gatewayservice")
	policy, err := MeshPolicyFromEnv()
	if err != nil || !policy.Enforce || policy.TrustDomain != "cluster.local" {
		t.Fatalf("policy=%+v err=%v, want enforcing policy with default trust domain", policy, err)
	}
}

func TestGRPCIdentityInterceptorBindsServicePrincipalToPeer(t *testing.T) {
	previous := meshPolicy
	meshPolicy = creditServiceMeshPolicy(t)
	defer func() { meshPolicy = previous }()

	call := func(xfcc string, pairs ...string) error {
		if xfcc != "" {
			pairs = append(pairs, "x-forwarded-client-cert", xfcc)
		}
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...))
		_, err := grpcIdentityInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/dancehub.credits.v1.CreditService/GetBalances"}, func(context.Context, any) (any, error) { return nil, nil })
		return err
	}
	if err := call(gatewayXFCC, "x-user-id", "user-1", "x-actor-kind", "human", "x-request-id", "request-1"); err != nil {
		t.Fatalf("gateway-forwarded human identity rejected: %v", err)
	}
	if err := call(gatewayXFCC, "x-actor-kind", "service", "x-service-principal", "scheduling-worker", "x-request-id", "request-2"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("gateway posing as scheduling-worker: %v, want Unauthenticated", err)
	}
	if err := call("", "x-user-id", "user-1", "x-actor-kind", "human", "x-request-id", "request-3"); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("call without mesh identity: %v, want Unauthenticated", err)
	}
	health := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "probe"))
	if _, err := grpcIdentityInterceptor(health, nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatalf("health check must not require a mesh peer: %v", err)
	}
}
