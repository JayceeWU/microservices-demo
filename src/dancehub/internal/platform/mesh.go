package platform

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MeshPolicy binds asserted identity metadata to the workload identity the service
// mesh proved during the mTLS handshake. Envoy records the peer certificate in the
// x-forwarded-client-cert header; with STRICT mTLS that certificate can only come from
// istiod, so its SPIFFE URI names the caller's ServiceAccount.
//
// Rules once enforcement is on:
//   - the peer must be one of this service's allowed callers (the same list that
//     renders the NetworkPolicy and AuthorizationPolicy in the Helm chart);
//   - a `service` actor may only claim a principal that is bound to the peer's
//     ServiceAccount, so a legitimate caller cannot impersonate another worker;
//   - human identities are accepted from any allowed caller, because domain services
//     forward the gateway-validated user through service-to-service hops.
type MeshPolicy struct {
	Enforce     bool
	TrustDomain string
	Namespace   string
	peers       map[string]map[string]bool
}

// MeshPolicyFromEnv reads MESH_PEER_ENFORCEMENT, MESH_TRUST_DOMAIN, MESH_NAMESPACE and
// MESH_ALLOWED_PEERS. Enforcement stays off unless explicitly enabled, which keeps Compose
// and the Kustomize development overlays (no mesh) working unchanged.
func MeshPolicyFromEnv() (MeshPolicy, error) {
	policy := MeshPolicy{
		Enforce:     strings.EqualFold(os.Getenv("MESH_PEER_ENFORCEMENT"), "true"),
		TrustDomain: strings.TrimSpace(os.Getenv("MESH_TRUST_DOMAIN")),
		Namespace:   strings.TrimSpace(os.Getenv("MESH_NAMESPACE")),
	}
	if !policy.Enforce {
		return policy, nil
	}
	if policy.TrustDomain == "" {
		policy.TrustDomain = "cluster.local"
	}
	if policy.Namespace == "" {
		return policy, fmt.Errorf("MESH_NAMESPACE is required when MESH_PEER_ENFORCEMENT is true")
	}
	peers, err := ParseMeshPeers(os.Getenv("MESH_ALLOWED_PEERS"))
	if err != nil {
		return policy, err
	}
	if len(peers) == 0 {
		return policy, fmt.Errorf("MESH_ALLOWED_PEERS must list at least one caller when MESH_PEER_ENFORCEMENT is true")
	}
	policy.peers = peers
	return policy, nil
}

// ParseMeshPeers parses "sa,sa=principal|principal,...": each entry names a caller's
// ServiceAccount and, optionally, the service principals that workload may assert.
func ParseMeshPeers(value string) (map[string]map[string]bool, error) {
	peers := map[string]map[string]bool{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		account, principalList, _ := strings.Cut(entry, "=")
		account = strings.TrimSpace(account)
		if account == "" {
			return nil, fmt.Errorf("MESH_ALLOWED_PEERS entry %q has no service account", entry)
		}
		principals := map[string]bool{}
		for _, principal := range strings.Split(principalList, "|") {
			if principal = strings.TrimSpace(principal); principal != "" {
				principals[principal] = true
			}
		}
		peers[account] = principals
	}
	return peers, nil
}

// PeerServiceAccount extracts the SPIFFE identity Envoy appended to
// x-forwarded-client-cert. Envoy appends the current client certificate as the last
// element, so the last element is the only one that cannot be forged by an upstream hop.
func PeerServiceAccount(header string) (trustDomain, namespace, serviceAccount string, ok bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", "", "", false
	}
	elements := strings.Split(header, ",")
	last := elements[len(elements)-1]
	for _, pair := range strings.Split(last, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "URI") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		rest, isSpiffe := strings.CutPrefix(value, "spiffe://")
		if !isSpiffe {
			continue
		}
		// <trust-domain>/ns/<namespace>/sa/<service-account>
		parts := strings.Split(rest, "/")
		if len(parts) != 5 || parts[1] != "ns" || parts[3] != "sa" || parts[0] == "" || parts[2] == "" || parts[4] == "" {
			return "", "", "", false
		}
		return parts[0], parts[2], parts[4], true
	}
	return "", "", "", false
}

// Authorize applies the policy to one inbound call.
func (p MeshPolicy) Authorize(xfcc, actorKind, servicePrincipal string) error {
	if !p.Enforce {
		return nil
	}
	trustDomain, namespace, account, ok := PeerServiceAccount(xfcc)
	if !ok {
		return status.Error(codes.Unauthenticated, "mesh peer identity is required")
	}
	if trustDomain != p.TrustDomain || namespace != p.Namespace {
		return status.Error(codes.PermissionDenied, "peer belongs to another trust domain or namespace")
	}
	principals, allowed := p.peers[account]
	if !allowed {
		return status.Error(codes.PermissionDenied, "peer workload is not an allowed caller")
	}
	if actorKind == "service" && !principals[servicePrincipal] {
		return status.Error(codes.Unauthenticated, "service principal is not bound to the peer workload")
	}
	return nil
}
